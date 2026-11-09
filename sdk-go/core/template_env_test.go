package core

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func executeTemplateEnvironment(t *testing.T, b *TemplateBuilder, config *ImageConfig, names []string) ([]string, map[string]any) {
	t.Helper()
	before, _ := b.JSON()
	spec, err := prepareImageSpec(context.Background(), b.Spec(), BuildOptions{ImageConfigResolver: func(context.Context, string, *RegistryConfig) (*ImageConfig, error) { return config, nil }})
	if err != nil {
		t.Fatal(err)
	}
	after, _ := b.JSON()
	if string(before) != string(after) {
		t.Fatal("preparation mutated builder")
	}
	steps, err := decodeInstructions(spec["instructions"])
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	profile := filepath.Join(dir, "profile")
	environment := filepath.Join(dir, "environment")
	_ = os.WriteFile(profile, []byte("export BASE='kept'\n"), 0600)
	_ = os.WriteFile(environment, []byte("BASE=kept\n"), 0600)
	rewrite := strings.NewReplacer("/etc/profile.d/99-image-env.sh", profile, "/etc/environment", environment, "/etc/.novita-image-env-base.sh", filepath.Join(dir, "base"), "/tmp/.novita-image-env.sh", filepath.Join(dir, "script"))
	env := map[string]string{"PATH": "/usr/bin:/bin", "A": "base", "LATER": "base", "v": "original"}
	for _, step := range steps {
		if strings.EqualFold(step.Type, "ENV") {
			for i := 0; i+1 < len(step.Args); i += 2 {
				env[step.Args[i]] = step.Args[i+1]
			}
			continue
		}
		if !strings.EqualFold(step.Type, "RUN") || len(step.Args) == 0 || !strings.Contains(step.Args[0], "image-env") {
			continue
		}
		command := step.Args[0]
		if strings.HasPrefix(command, "echo ") {
			parts := strings.SplitN(command, " ", 3)
			decoded, err := base64.StdEncoding.DecodeString(strings.Trim(parts[1], "'"))
			if err != nil {
				t.Fatal(err)
			}
			parts[1] = base64.StdEncoding.EncodeToString([]byte(rewrite.Replace(string(decoded))))
			command = strings.Join(parts, " ")
		}
		cmd := exec.Command("/bin/sh", "-c", rewrite.Replace(command))
		for k, v := range env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("environment script: %v %s", err, out)
		}
	}
	var query strings.Builder
	fmt.Fprintf(&query, ". %s; ", shellQuote(profile))
	for _, name := range names {
		fmt.Fprintf(&query, "printf '%%s\\000' \"${%s-}\"; ", name)
	}
	cmd := exec.Command("/bin/sh", "-c", query.String())
	cmd.Env = []string{"PATH=/different", "LATER=runtime"}
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00"), spec
}

func TestTemplateEnvironmentScopeAndGroups(t *testing.T) {
	b := NewTemplateBuilder().FromDockerfile("FROM example/image\nARG ROOT=/first\nENV SAVE=$ROOT\nARG ROOT=/second\nENV A=first\nENV A=second B=$A\nENV PRICE=\"hello B=world\" WORDS=\"two words\"\n")
	got, _ := executeTemplateEnvironment(t, b, &ImageConfig{}, []string{"SAVE", "ROOT", "A", "B", "PRICE", "WORDS", "BASE"})
	want := []string{"/first", "", "second", "first", "hello B=world", "two words", "kept"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
	b = NewTemplateBuilder().FromImage("image").Env("TOKEN", "secret")
	got, _ = executeTemplateEnvironment(t, b, &ImageConfig{}, []string{"TOKEN"})
	if got[0] != "" {
		t.Fatalf("build-only token persisted: %q", got)
	}
}

func TestTemplateEnvironmentPreservesLiteralsOrderAndEmptyValues(t *testing.T) {
	b := NewTemplateBuilder().FromImage("image").Env("PATH", "/one:$PATH", true).Env("PATH", "/two:$PATH", true).Env("SAVE", "$LATER", true).Env("LATER", "build-secret").Env("v", "quoted'\n\n", true).Env("EMPTY", "", true)
	got, _ := executeTemplateEnvironment(t, b, &ImageConfig{Env: map[string]string{"PATH": "/image:/usr/bin:/bin", "PRICE": "cost$5"}}, []string{"PATH", "SAVE", "LATER", "v", "EMPTY", "PRICE", "BASE"})
	want := []string{"/two:/one:/image:/usr/bin:/bin", "base", "runtime", "quoted'\n\n", "", "cost$5", "kept"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
	for _, path := range []string{"", "/custom/bin"} {
		b := NewTemplateBuilder().FromImage("image").Env("PATH", path, true).Env("APP", "works", true)
		got, _ := executeTemplateEnvironment(t, b, &ImageConfig{}, []string{"PATH", "APP"})
		if !reflect.DeepEqual(got, []string{path, "works"}) {
			t.Fatal(got)
		}
	}
}

func TestTemplateInheritedWorkdirPrecedesCallerSteps(t *testing.T) {
	b := NewTemplateBuilder().FromImage("image").Run("pwd").Workdir("/override").Run("pwd")
	_, spec := executeTemplateEnvironment(t, b, &ImageConfig{Workdir: "/image"}, nil)
	steps, _ := decodeInstructions(spec["instructions"])
	if len(steps) != 4 || steps[0].Type != "WORKDIR" || steps[0].Args[0] != "/image" || steps[1].Type != "RUN" || steps[2].Args[0] != "/override" {
		t.Fatalf("%+v", steps)
	}
}

func TestTemplateEnvironmentQuotedBackslashesAndDollars(t *testing.T) {
	b := NewTemplateBuilder().FromDockerfile("FROM image\nENV REGEX='\\d+' DOUBLE=\"a\\b\"\n").Env("PRICE", "cost$5 $$ $? ${MISSING:-literal}", true)
	got, _ := executeTemplateEnvironment(t, b, &ImageConfig{}, []string{"REGEX", "DOUBLE", "PRICE"})
	if want := []string{`\d+`, `a\b`, "cost$5 $$ $? ${MISSING:-literal}"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}
