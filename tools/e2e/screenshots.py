# Regenerates the screenshots of the README (docs/screenshots/), in English.
# Start the harness first:  go run ./tools/e2e > /tmp/e2e.log &
# then:                     python3 tools/e2e/screenshots.py
import json, os, time, urllib.request
from playwright.sync_api import sync_playwright

info = json.loads(open('/tmp/e2e.log').readline().split(' ', 1)[1])
BASE = 'http://127.0.0.1:7980/'
OUT = os.path.join(os.path.dirname(__file__), '..', '..', 'docs', 'screenshots')
os.makedirs(OUT, exist_ok=True)

def call(method, path, body=None):
    req = urllib.request.Request(BASE + 'api' + path, method=method, data=json.dumps(body).encode() if body is not None else None,
                                 headers={'Content-Type': 'application/json', 'X-Cinearr': '1'})
    with urllib.request.urlopen(req, timeout=180) as r:
        return json.loads(r.read() or b'null')

st = call('GET', '/settings')
st.update({'setupDone': True, 'tmdbKey': 'demo', 'uiLanguage': 'en', 'libraryPath': info['library'], 'seriesPath': info['series']})
st['downloadClient'].update({'kind': 'qbittorrent', 'url': info['qbit'], 'username': 'admin', 'password': 'secret'})
st['subtitles']['providers'] = [{'id': 'opensubtitles', 'enabled': True, 'settings': {'username': 'me', 'password': 'x', 'apiKey': 'x'}},
                                {'id': 'yifysubtitles', 'enabled': True, 'settings': {}}]
call('PUT', '/settings', st)
call('POST', '/indexers', {'kind': 'cardigann', 'definitionId': 'demotracker', 'name': 'Demo Tracker', 'enabled': True, 'priority': 25, 'settings': {}})
call('POST', '/movies', {'tmdbId': 27205})
call('POST', '/series', {'tmdbId': 1920})
call('POST', '/series/1920/autosearch', {'episodes': [{'season': 2, 'episode': e} for e in (1, 2, 3)]})
call('POST', '/movies/27205/autosearch')

with sync_playwright() as p:
    b = p.chromium.launch()
    pg = b.new_page(viewport={'width': 1440, 'height': 900})
    def shot(name):
        time.sleep(.6)
        pg.screenshot(path=os.path.join(OUT, name + '.png'))
    time.sleep(4)
    pg.goto(BASE + '#/downloads'); pg.wait_for_selector('#dl .item'); shot('downloads')
    pg.goto(BASE + '#/series/1920'); pg.wait_for_selector('table.eps')
    pg.click('[data-act=season][data-n="2"]')
    for e in (4, 5, 6):
        pg.click(f'[data-act=ep-check][data-k="2:{e}"]')
    shot('series')
    pg.click('[data-act=season-manual]'); pg.wait_for_selector('.modal table', timeout=60000); shot('season-search')
    pg.keyboard.press('Escape')
    pg.goto(BASE + '#/movie/27205'); pg.wait_for_selector('.hero h1')
    pg.click('.hero [data-act=manual]'); pg.wait_for_selector('.modal table', timeout=60000); shot('manual-search')
    pg.keyboard.press('Escape')
    pg.goto(BASE + '#/settings/subtitles'); pg.wait_for_selector('#sub-providers .item'); shot('settings')
    b.close()
print('screenshots written to', os.path.abspath(OUT))
