# Exercises the series flow through the API (no browser needed). Run it
# through tools/e2e/run.sh, or start the harness first:
#   PATH=$PWD/tools/e2e/fakebin:$PATH go run ./tools/e2e > /tmp/e2e.log &
#   python3 tools/e2e/series_api.py
# Adds Twin Peaks, grabs S02E01-E03 one by one, then the season pack for the
# rest of season 2, and checks the files imported into the series folder.
import json, time, urllib.request, os, sys
info = json.loads(open('/tmp/e2e.log').readline().split(' ', 1)[1])
B = 'http://127.0.0.1:7980/api'
def call(method, path, body=None):
    req = urllib.request.Request(B + path, method=method, data=json.dumps(body).encode() if body is not None else None,
        headers={'Content-Type': 'application/json', 'X-Cinearr': '1'})
    try:
        with urllib.request.urlopen(req, timeout=180) as r:
            return json.loads(r.read() or b'null')
    except urllib.error.HTTPError as e:
        raise SystemExit(f'{method} {path}: {e.code} {e.read().decode()}')
st = call('GET', '/settings')
st.update({'setupDone': True, 'tmdbKey': 'demo', 'uiLanguage': 'en', 'libraryPath': info['library'], 'seriesPath': info['series']})
st['downloadClient'].update({'kind': 'qbittorrent', 'url': info['qbit'], 'username': 'a', 'password': 'b'})
st['profile']['requiredLanguages'] = []
call('PUT', '/settings', st)
call('POST', '/indexers', {'kind': 'cardigann', 'definitionId': 'demotracker', 'name': 'Demo', 'enabled': True, 'priority': 25, 'settings': {}})
r = call('GET', '/lookup?kind=tv&q=twin')
assert r['results'][0]['kind'] == 'tv', r
d = call('GET', '/discover?kind=tv&type=trending')
assert all(x['title'] != 'Future Show' for x in d['results']), 'unaired series listed'
s = call('POST', '/series', {'tmdbId': 1920})
print('added', s['title'], s['seasons'], 'seasons', s['episodes'], 'episodes')
full = call('GET', '/series/1920')
assert [x['number'] for x in full['seasons']] == [0, 1, 2, 3], [x['number'] for x in full['seasons']]
assert len(full['seasons'][2]['episodes']) == 22
# manual search for the season: the pack is accepted (whole season missing)
m = call('POST', '/series/1920/search?season=2&episode=0')
for x in m['decisions']:
    print(('OK ' if x['approved'] else 'NO '), x['release']['title'], x.get('label'), x.get('rejections'))
pack = [x for x in m['decisions'] if x['release']['title'] == 'Twin.Peaks.S02.1080p.BluRay.x264-SHORTBREHD'][0]
assert pack['approved'], pack
# automatic search for 3 episodes: three singles, the EVIL one blocked, no pack
call('POST', '/series/1920/autosearch', {'episodes': [{'season': 2, 'episode': e} for e in (1, 2, 3)]})
for i in range(90):
    time.sleep(1)
    full = call('GET', '/series/1920')
    eps = full['seasons'][2]['episodes'][:4]
    sts = [e.get('status') for e in eps]
    if sts[:3] == ['available'] * 3:
        break
print('statuses', sts, [e.get('statusMessage') for e in eps])
assert sts[:3] == ['available'] * 3, sts
assert sts[3] in (None, ''), sts
for e in eps[:3]:
    print(' ', e['file']['path'])
    assert os.path.exists(e['file']['path'])
h = call('GET', '/history')
print('\n'.join(f"{x['kind']}: {x['message']}" for x in h if x.get('seriesId') == 1920))
bl = call('GET', '/blocklist')
print('blocklist', [(x['title'], x.get('seriesId')) for x in (bl or [])])
# play an episode
call('POST', '/series/1920/play?season=2&episode=1')
# cancel flow: grab the 720p for episode 4? use manual grab of S02E01 720p would be an upgrade check; skip
print('series list', call('GET', '/series')[0])
# whole season 2 except the 3 imported: the pack covers 19 missing episodes -> accepted
call('POST', '/series/1920/autosearch', {'episodes': [{'season': 2, 'episode': e} for e in range(4, 23)]})
for i in range(120):
    time.sleep(1)
    full = call('GET', '/series/1920')
    sts = [e.get('status') for e in full['seasons'][2]['episodes']]
    if all(x == 'available' for x in sts):
        break
print('season 2', sts.count('available'), 'available')
assert all(x == 'available' for x in sts), sts
# the 3 first keep their file (same quality, pack not better)
print(full['seasons'][2]['episodes'][0]['file']['releaseTitle'], '|', full['seasons'][2]['episodes'][5]['file']['releaseTitle'])
# selecting 2 episodes of season 1: no pack in the fake, singles not found -> message
call('POST', '/series/1920/autosearch', {'episodes': [{'season': 1, 'episode': 1}, {'season': 1, 'episode': 2}]})
time.sleep(4)
full = call('GET', '/series/1920')
print('S1', [(e.get('status'), e.get('statusMessage')) for e in full['seasons'][1]['episodes'][:3]])
h = call('GET', '/history')
print('\n'.join(f"{x['kind']}: {x['message']}" for x in h[:6] if x.get('seriesId') == 1920))
print('ALL OK')
