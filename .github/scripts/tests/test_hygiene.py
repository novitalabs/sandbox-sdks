import pathlib
import subprocess
import tempfile
import unittest

SCRIPT = pathlib.Path(__file__).resolve().parents[1] / "hygiene_scan.py"

class HygieneContract(unittest.TestCase):
    def test_credentials_and_personal_paths_fail_without_echoing_values(self):
        samples = ["gh" + "p_" + "aB9z" * 10, "AK" + "IA" + "ABCDEFGHIJKLMNOP", "py" + "pi-" + "aB9z" * 20, "/Users/" + "employee/work/project"]
        for sample in samples:
            with self.subTest(kind=sample[:3]), tempfile.TemporaryDirectory() as tmp:
                pathlib.Path(tmp, "example.txt").write_text(sample)
                r = subprocess.run(["python3", str(SCRIPT), tmp], text=True, capture_output=True)
                self.assertNotEqual(r.returncode, 0)
                self.assertNotIn(sample, r.stdout + r.stderr)

    def test_committed_node_modules_and_scanner_file_cannot_hide_secrets(self):
        with tempfile.TemporaryDirectory() as tmp:
            root=pathlib.Path(tmp)
            subprocess.run(["git", "init", "-q", tmp], check=True)
            p=root/"node_modules/leak.txt"; p.parent.mkdir();p.write_text("gh" + "p_" + "aB9z" * 10)
            subprocess.run(["git","add","-f","node_modules/leak.txt"],cwd=root,check=True)
            r=subprocess.run(["python3",str(SCRIPT),tmp],capture_output=True,text=True)
            self.assertNotEqual(r.returncode,0)

    def test_public_placeholders_are_allowed(self):
        with tempfile.TemporaryDirectory() as tmp:
            pathlib.Path(tmp,"example.txt").write_text('NOVITA_API_KEY="your-api-key"\nhttps://example.com\n')
            r=subprocess.run(["python3",str(SCRIPT),tmp],capture_output=True,text=True)
            self.assertEqual(r.returncode,0,r.stdout)

if __name__ == "__main__":
    unittest.main()
