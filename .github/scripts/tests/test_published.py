import base64
import hashlib
import importlib.util
import io
import json
import os
import pathlib
import subprocess
import sys
import tempfile
import unittest
from contextlib import redirect_stdout
from unittest.mock import patch

SCRIPTS = pathlib.Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('verify_published', SCRIPTS / 'verify-published.py')
published = importlib.util.module_from_spec(spec)
spec.loader.exec_module(published)

class PublishedGoModule(unittest.TestCase):
    module = 'github.com/novitalabs/sandbox-sdks/sdk-go/v2'
    version = 'v2.1.2-alpha.1'
    token = 'fixture-private-go-token'

    def setUp(self):
        self.calls = []

    def download(self, args, **kwargs):
        self.assertEqual(args, ['go', 'mod', 'download', '-json', self.module + '@' + self.version])
        self.assertIn('env', kwargs, 'download must use an isolated process environment')
        env = kwargs['env']
        cwd = pathlib.Path(kwargs['cwd'])
        cache = pathlib.Path(env['GOMODCACHE'])
        self.assertTrue(cwd.is_dir())
        self.assertEqual(list(cwd.iterdir()), [], 'download must start outside the repository')
        self.assertFalse(cache.exists(), 'download must not reuse a module cache')
        self.assertTrue(kwargs['capture_output'], 'tool output must not leak credentials')
        self.assertTrue(kwargs['text'])
        self.calls.append((args, kwargs))
        directory = cache / 'github.com/novitalabs/sandbox-sdks/sdk-go/v2@v2.1.2-alpha.1'
        (directory / 'core').mkdir(parents=True)
        (directory / 'core/connection_config.go').write_text('package core\nconst SDKVersion = "2.1.2-alpha.1"\n')
        data = {'Path': self.module, 'Version': self.version, 'Dir': str(directory),
                'Info': str(cache / 'cache/download/module.info'),
                'GoMod': str(cache / 'cache/download/module.mod'),
                'Zip': str(cache / 'cache/download/module.zip'),
                'Sum': 'h1:fixture', 'GoModSum': 'h1:fixture'}
        return subprocess.CompletedProcess(args, 0, json.dumps(data), '')

    def run_main(self, env, download=None, output=None):
        output = output if output is not None else io.StringIO()
        with patch.dict(os.environ, env, clear=True), \
                patch.object(sys, 'argv', ['verify-published.py', 'sdk-go/v2.1.2-alpha.1']), \
                patch.object(published.subprocess, 'run', side_effect=download or self.download), \
                redirect_stdout(output):
            published.main()
            self.assertEqual(dict(os.environ), env, 'verification must not mutate its parent environment')
        return output.getvalue()

    def test_public_download_uses_public_verification_and_fresh_cache(self):
        env = {'REPOSITORY_PRIVATE': 'false', 'GOPRIVATE': '*', 'GONOPROXY': '*',
               'GONOSUMDB': '*', 'GOSUMDB': 'off', 'GOPROXY': 'off',
               'GOMODCACHE': '/fixture/existing-cache', 'GOFLAGS': '-modfile=local.mod',
               'GOWORK': '/fixture/local.work', 'GH_TOKEN': self.token}
        self.assertIn(self.module, self.run_main(env))
        self.run_main(env)
        caches = []
        for args, kwargs in self.calls:
            child = kwargs['env']
            self.assertEqual(child['GOPROXY'], 'https://proxy.golang.org,direct')
            self.assertEqual(child['GOSUMDB'], 'sum.golang.org')
            for key in ('GOPRIVATE', 'GONOPROXY', 'GONOSUMDB'):
                self.assertEqual(child[key], '')
            self.assertEqual(child['GOENV'], 'off')
            self.assertEqual(child['GOWORK'], 'off')
            self.assertNotIn('modfile', child['GOFLAGS'])
            self.assertNotIn('GH_TOKEN', child)
            self.assertNotIn('gh auth git-credential', str(child))
            self.assertNotEqual(child['GOMODCACHE'], env['GOMODCACHE'])
            self.assertFalse(pathlib.Path(child['GOMODCACHE']).exists(), 'temporary cache must be removed')
            self.assertFalse(pathlib.Path(kwargs['cwd']).exists())
            caches.append(child['GOMODCACHE'])
        self.assertNotEqual(caches[0], caches[1])

    def test_private_download_uses_repository_scoped_bypass_and_ephemeral_auth(self):
        output = self.run_main({'REPOSITORY_PRIVATE': 'true', 'GITHUB_REPOSITORY': 'novitalabs/sandbox-sdks',
                                'GH_TOKEN': self.token, 'GOPRIVATE': '*', 'GOSUMDB': 'off'})
        args, kwargs = self.calls[0]
        child = kwargs['env']
        for key in ('GOPRIVATE', 'GONOPROXY', 'GONOSUMDB'):
            self.assertEqual(child[key], 'github.com/novitalabs/sandbox-sdks')
        self.assertEqual(child['GOPROXY'], 'https://proxy.golang.org,direct')
        self.assertEqual(child['GOSUMDB'], 'sum.golang.org')
        self.assertEqual(child['GH_TOKEN'], self.token)
        self.assertEqual(child['GIT_TERMINAL_PROMPT'], '0')
        config = [(child[f'GIT_CONFIG_KEY_{i}'], child[f'GIT_CONFIG_VALUE_{i}'])
                  for i in range(int(child['GIT_CONFIG_COUNT']))]
        self.assertEqual(config[0], ('credential.https://github.com.helper', ''))
        self.assertIn(('credential.https://github.com.helper', '!gh auth git-credential'), config)
        self.assertNotIn(self.token, str(args) + str(config) + output)

    def test_private_download_requires_token_and_matching_repository(self):
        for env in ({'REPOSITORY_PRIVATE': 'true', 'GITHUB_REPOSITORY': 'novitalabs/sandbox-sdks'},
                    {'REPOSITORY_PRIVATE': 'true', 'GITHUB_REPOSITORY': 'someone/another', 'GH_TOKEN': self.token}):
            with self.subTest(env=env), self.assertRaises(ValueError):
                self.run_main(env)
        self.assertEqual(self.calls, [])

    def test_repository_visibility_must_be_explicit(self):
        for value in (None, '', 'yes'):
            env = {} if value is None else {'REPOSITORY_PRIVATE': value}
            with self.subTest(value=value), self.assertRaises(ValueError):
                self.run_main(env)
        self.assertEqual(self.calls, [])

    def test_failed_authenticated_download_cannot_announce_success_or_leak_output(self):
        def denied(args, **kwargs):
            return subprocess.CompletedProcess(args, 1, self.token, 'authentication failed: ' + self.token)
        output = io.StringIO()
        with redirect_stdout(output), self.assertRaises(ValueError) as error:
            self.run_main({'REPOSITORY_PRIVATE': 'true', 'GITHUB_REPOSITORY': 'novitalabs/sandbox-sdks',
                           'GH_TOKEN': self.token}, denied, output)
        self.assertNotIn(self.token, str(error.exception) + output.getvalue())
        self.assertNotIn('Verified', output.getvalue())

    def test_downloaded_module_metadata_must_match_tag(self):
        invalid = [{'Path': 'github.com/someone/another/v2'}, {'Version': 'v2.1.2'},
                   {'Replace': {'Path': '/fixture/local'}}, {'Error': 'remote module unavailable'},
                   {'Dir': '/fixture/existing-cache'}]
        for change in invalid:
            def corrupt(args, **kwargs):
                result = self.download(args, **kwargs)
                data = json.loads(result.stdout)
                data.update(change)
                result.stdout = json.dumps(data)
                return result
            with self.subTest(change=change), self.assertRaises(ValueError):
                self.run_main({'REPOSITORY_PRIVATE': 'false'}, corrupt)

    def test_downloaded_sdk_version_must_match_tag(self):
        def corrupt(args, **kwargs):
            result = self.download(args, **kwargs)
            directory = pathlib.Path(json.loads(result.stdout)['Dir'])
            (directory / 'core/connection_config.go').write_text('package core\nconst SDKVersion = "2.1.2"\n')
            return result
        with self.assertRaises(ValueError):
            self.run_main({'REPOSITORY_PRIVATE': 'false'}, corrupt)
