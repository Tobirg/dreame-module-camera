"""Run the camera module handler as the agent would, against a temporary photo folder."""
from pathlib import Path
import base64, json, os, subprocess, tempfile, unittest

HANDLER = Path(__file__).resolve().parents[1] / 'package' / 'handler'


class HandlerTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        t = Path(self.tmp.name)
        self.photos, self.data, self.live = t / 'photos', t / 'data', t / 'live'
        self.photos.mkdir()

    def tearDown(self):
        self.tmp.cleanup()

    def run_handler(self, action, **params):
        env = {'PATH': os.environ['PATH'], 'DREAME_MODULE_DATA': str(self.data), 'CAMERA_PHOTOS': str(self.photos), 'CAMERA_LIVE': str(self.live)}
        env.update(getattr(self, 'extra_env', {}))
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

    def test_live(self):
        self.assertEqual(self.run_handler('live')[0], 1, 'no live view while off')
        self.run_handler('set', enabled=True)
        self.assertEqual(self.run_handler('live')[1]['state'], 'starting')
        self.assertTrue((self.live / 'want').exists(), 'asks the service to capture')
        (self.live / 'status.json').write_text('{"state":"error","reason":"asleep"}')
        self.assertEqual(self.run_handler('live')[1]['reason'], 'asleep')
        (self.live / 'live.jpg').write_bytes(b'\xff\xd8live')
        code, f = self.run_handler('live')
        self.assertEqual(base64.b64decode(f['jpeg']), b'\xff\xd8live')
        old = (self.live / 'live.jpg')
        os.utime(old, (1000, 1000))
        self.assertNotIn('jpeg', self.run_handler('live')[1], 'a stale frame is not shown')

    def test_light(self):
        self.assertTrue(self.run_handler('light', on=True)[1]['light'])
        self.assertTrue((self.data / 'light').exists(), 'the service reads this flag')
        self.assertFalse(self.run_handler('light', on=False)[1]['light'])
        self.assertEqual(self.run_handler('light', on='maybe')[0], 1)

    def test_video_code(self):
        t = Path(self.tmp.name)
        # Mimics r2505 4.3.9_2021: no "session" -> -1; 1104 sets/removes the allow flag; 1101 keeps the hash.
        avacmd = t / 'avacmd'
        avacmd.write_text(f'''#!/usr/bin/env python3
import json,os,sys
root={str(t)!r}
r=json.loads(sys.argv[2]); open(root+'/requests.log','a').write(sys.argv[2]+'\\n')
p=r['params']['in'][0]; v=json.loads(p['value']); out={{'piid':p['piid']}}; code=root+'/code'
cur=open(code).read() if os.path.exists(code) else None
if 'session' not in v: out['value']=-1
elif p['piid']==1100: out['value']=1 if cur else 0
elif p['piid']==1101:
    out['value']=1 if cur==v.get('oldcode') else 0
    if out['value']: open(code,'w').write(v['newcode'])
elif p['piid']==1104:
    if v['value']==1: open(root+'/flag','w').close()
    elif os.path.exists(root+'/flag'): os.remove(root+'/flag')
print('firmware log'); print(json.dumps({{'id':r['id'],'result':dict(r['params'],out=[out],code=0)}}))
''')
        avacmd.chmod(0o755)
        (t / 'device.conf').write_text('model=dreame.vacuum.r2505\ndid=12345678\n')
        log = t / 'avaexec.log'
        log.write_text('start: 20261123-171936.356, /usr/bin/ACLCodeOps.sh &\n')  # a press before allowing
        env = {'CAMERA_AVACMD': str(avacmd), 'CAMERA_DEVICE_CONF': str(t / 'device.conf'), 'CAMERA_ACL_LOG': str(log), 'CAMERA_ACL_FLAG': str(t / 'flag')}
        self.extra_env = env
        self.assertEqual(self.run_handler('code')[1], {'code_set': False, 'waiting': False, 'reset': False})
        self.assertTrue(self.run_handler('code', enabled=True)[1]['waiting'])
        self.assertTrue(self.run_handler('code')[1]['waiting'], 'an old press does not count')
        with log.open('a') as f:
            f.write('start: 20261123-171955.711, /usr/bin/ACLCodeOps.sh &\nend : 20261123-171955.715, /usr/bin/ACLCodeOps.sh &\n')
        self.assertEqual(self.run_handler('code')[1], {'code_set': False, 'waiting': False, 'reset': True})
        self.assertFalse((t / 'flag').exists(), 'the reset is forbidden again after the press')
        self.assertTrue(self.run_handler('code', enabled=True)[1]['waiting'])
        self.assertFalse(self.run_handler('code', enabled=False)[1]['waiting'])
        self.assertEqual(self.run_handler('code_set', code='12a4')[0], 1)
        code, c = self.run_handler('code_set', code='1234')
        self.assertEqual((code, c['code_set']), (0, True))
        self.assertEqual((t / 'code').read_text(), '03ac674216f3e15c761ee1a5e255f067953623c8b388b4459e13f978d7c846f4')
        self.assertEqual(self.run_handler('code_set', code='5678', old_code='0000')[0], 1)
        self.assertEqual(self.run_handler('code_set', code='5678', old_code='1234')[0], 0)
        self.assertNotIn('"value":-1', (t / 'requests.log').read_text())

    def test_manifest_fits_the_agent(self):
        m = json.loads((HANDLER.parent / 'manifest.json').read_text(encoding='utf-8'))
        self.assertLessEqual(len(m['description'].encode()), 500, 'the agent refuses longer descriptions')

    def test_no_folder(self):
        self.photos.rmdir()
        self.assertEqual(self.run_handler('status')[1], {'enabled': False, 'light': False, 'folder': False, 'photos': []})


if __name__ == '__main__':
    unittest.main()
