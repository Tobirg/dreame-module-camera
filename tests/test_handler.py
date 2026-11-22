"""Run the camera module handler as the agent would, against a temporary photo folder."""
from pathlib import Path
import base64, json, os, subprocess, tempfile, unittest

HANDLER = Path(__file__).resolve().parents[1] / 'package' / 'handler'


class HandlerTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        t = Path(self.tmp.name)
        self.photos, self.data = t / 'photos', t / 'data'
        self.photos.mkdir()

    def tearDown(self):
        self.tmp.cleanup()

    def run_handler(self, action, **params):
        env = {'PATH': os.environ['PATH'], 'DREAME_MODULE_DATA': str(self.data), 'CAMERA_PHOTOS': str(self.photos)}
        env.update({f'P_{k}': str(v).lower() if isinstance(v, bool) else str(v) for k, v in params.items()})
        p = subprocess.run(['sh', str(HANDLER), action], env=env, capture_output=True, text=True)
        return p.returncode, json.loads(p.stdout)

    def photo(self, name, at, body=b'\xff\xd8jpeg'):
        f = self.photos / name
        f.write_bytes(body)
        os.utime(f, (at, at))

    def test_off_by_default_and_newest_first(self):
        self.photo('1_0.5_14400.jpg', 1000)
        self.photo('2_-1.2_9600.jpg', 2000, b'\xff\xd8new')
        self.photo('bad name.jpg', 3000)
        code, s = self.run_handler('status')
        self.assertEqual((code, s['enabled'], s['folder']), (0, False, True))
        self.assertEqual(s['photos'], [{'name': '2_-1.2_9600.jpg', 'at': 2000}, {'name': '1_0.5_14400.jpg', 'at': 1000}])
        self.assertEqual(self.run_handler('photo')[0], 1, 'photos are not served while off')
        self.assertTrue(self.run_handler('set', enabled=True)[1]['enabled'])
        code, p = self.run_handler('photo')
        self.assertEqual((code, p['name'], base64.b64decode(p['jpeg'])), (0, '2_-1.2_9600.jpg', b'\xff\xd8new'))
        self.assertEqual(self.run_handler('photo', name='1_0.5_14400.jpg')[1]['at'], 1000)
        for bad in ('../x.jpg', 'x.png', 'gone.jpg'):
            self.assertEqual(self.run_handler('photo', name=bad)[0], 1, bad)
        self.assertFalse(self.run_handler('set', enabled=False)[1]['enabled'])

    def test_no_folder(self):
        self.photos.rmdir()
        self.assertEqual(self.run_handler('status')[1], {'enabled': False, 'folder': False, 'photos': []})


if __name__ == '__main__':
    unittest.main()
