# Regenerates the screenshots of the README (docs/screenshots/), in English,
# with the demonstration data: open films of the Blender Foundation, public-
# domain classics and a Creative Commons series. Posters, backdrops and
# photos are drawn here (simple generated artwork), no image is downloaded.
#
#   E2E_DEMO=1 PATH="$PWD/tools/e2e/fakebin:$PATH" go run ./tools/e2e > /tmp/e2e.log &
#   python3 tools/e2e/screenshots.py
#
# Needs Python 3, Pillow and Playwright (pip install pillow playwright &&
# playwright install chromium).
import hashlib, io, json, os, re, time, urllib.request
from PIL import Image, ImageDraw, ImageFilter, ImageFont
from playwright.sync_api import sync_playwright

info = json.loads(open('/tmp/e2e.log').readline().split(' ', 1)[1])
BASE = 'http://127.0.0.1:7980/'
OUT = os.path.join(os.path.dirname(__file__), '..', '..', 'docs', 'screenshots')
os.makedirs(OUT, exist_ok=True)

TITLES = {45745: ('Sintel', 2010), 10378: ('Big Buck Bunny', 2008), 133701: ('Tears of Steel', 2012),
          9761: ('Elephants Dream', 2006), 358332: ('Cosmos Laundromat', 2015), 10331: ('Night of the Living Dead', 1968),
          653: ('Nosferatu', 1922), 961: ('The General', 1926), 33838: ('Pioneer One', 2010)}
PALETTES = [((20, 32, 74), (214, 96, 52)), ((12, 58, 64), (233, 196, 106)), ((48, 18, 64), (240, 128, 128)),
            ((16, 24, 32), (120, 170, 200)), ((70, 20, 28), (250, 180, 90)), ((10, 40, 30), (150, 210, 140)),
            ((34, 34, 50), (200, 200, 220)), ((60, 40, 20), (240, 220, 170)), ((18, 30, 60), (120, 140, 255))]


def font(size, bold=True):
    for f in ['/usr/share/fonts/opentype/inter/InterDisplay-Bold.otf', '/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf',
              'C:/Windows/Fonts/segoeuib.ttf', '/System/Library/Fonts/Helvetica.ttc']:
        if os.path.exists(f):
            return ImageFont.truetype(f, size)
    return ImageFont.load_default(size)


def palette(key):
    return PALETTES[int(hashlib.md5(str(key).encode()).hexdigest(), 16) % len(PALETTES)]


def gradient(w, h, top, bottom):
    img = Image.new('RGB', (w, h))
    d = ImageDraw.Draw(img)
    for y in range(h):
        t = y / max(h - 1, 1)
        d.line([(0, y), (w, y)], fill=tuple(int(a + (b - a) * t) for a, b in zip(top, bottom)))
    return img


def scene(w, h, key):
    dark, light = palette(key)
    img = gradient(w, h, dark, tuple(int(c * .55) for c in light))
    glow = Image.new('RGBA', (w, h), (0, 0, 0, 0))
    g = ImageDraw.Draw(glow)
    r = int(min(w, h) * .28)
    cx, cy = int(w * .62), int(h * .38)
    g.ellipse([cx - r, cy - r, cx + r, cy + r], fill=light + (210,))
    glow = glow.filter(ImageFilter.GaussianBlur(r // 6))
    img.paste(glow, (0, 0), glow)
    d = ImageDraw.Draw(img)
    for i, k in enumerate((.68, .78, .9)):  # hills
        y = int(h * k)
        shade = tuple(int(c * (.35 - i * .1)) for c in dark)
        d.polygon([(0, y), (w * .3, y - h * .06), (w * .7, y + h * .02), (w, y - h * .05), (w, h), (0, h)], fill=shade)
    return img


def poster(key):
    title, year = TITLES.get(key, ('', 0))
    img = scene(500, 750, key)
    d = ImageDraw.Draw(img)
    words, lines = title.upper().split(), ['']
    f = font(64)
    for wd in words:
        trial = (lines[-1] + ' ' + wd).strip()
        if d.textlength(trial, font=f) > 430 and lines[-1]:
            lines.append(wd)
        else:
            lines[-1] = trial
    y = 750 - 70 - 70 * len(lines)
    d.text((250, y - 34), str(year), font=font(26), fill=(235, 235, 240), anchor='mm')
    for ln in lines:
        d.text((250, y + 30), ln, font=f, fill=(250, 250, 252), anchor='mm')
        y += 70
    return img


def portrait(key):
    img = gradient(185, 278, *palette(key + 7))
    d = ImageDraw.Draw(img)
    shade = tuple(int(c * .45) for c in palette(key + 7)[0])
    d.ellipse([52, 60, 133, 141], fill=shade)
    d.ellipse([22, 150, 163, 320], fill=shade)
    return img


def image_for(path):
    m = re.search(r'/demo/(poster|backdrop|person)-(\d+)\.png', path)
    kind, key = (m.group(1), int(m.group(2))) if m else ('poster', 0)
    img = poster(key) if kind == 'poster' else scene(1280, 720, key) if kind == 'backdrop' else portrait(key)
    buf = io.BytesIO()
    img.save(buf, 'PNG')
    return buf.getvalue()


cache = {}


def serve_image(route):
    url = route.request.url
    if url not in cache:
        cache[url] = image_for(url)
    route.fulfill(status=200, content_type='image/png', body=cache[url])


def call(method, path, body=None):
    req = urllib.request.Request(BASE + 'api' + path, method=method, data=json.dumps(body).encode() if body is not None else None,
                                 headers={'Content-Type': 'application/json', 'X-Cinearr': '1'})
    with urllib.request.urlopen(req, timeout=180) as r:
        return json.loads(r.read() or b'null')


st = call('GET', '/settings')
st.update({'setupDone': True, 'tmdbKey': 'demo', 'uiLanguage': 'en', 'language': 'en-US',
           'libraryPath': info['library'], 'seriesPath': info['series']})
st['downloadClient'].update({'kind': 'qbittorrent', 'url': info['qbit'], 'username': 'admin', 'password': 'secret'})
st['subtitles']['providers'] = [{'id': 'opensubtitles', 'enabled': True, 'settings': {'username': 'me', 'password': 'x', 'apiKey': 'x'}},
                                {'id': 'yifysubtitles', 'enabled': True, 'settings': {}}]
call('PUT', '/settings', st)
call('POST', '/indexers', {'kind': 'cardigann', 'definitionId': 'demotracker', 'name': 'Demo Tracker', 'enabled': True, 'priority': 25, 'settings': {}})
for film in (45745, 10331, 653, 133701, 961, 10378):
    call('POST', '/movies', {'tmdbId': film})
call('POST', '/series', {'tmdbId': 33838})
for film in (45745, 653, 133701):
    call('POST', f'/movies/{film}/autosearch')
call('POST', '/series/33838/autosearch', {'episodes': [{'season': 1, 'episode': e} for e in (1, 2, 3)]})

with sync_playwright() as p:
    b = p.chromium.launch()
    pg = b.new_page(viewport={'width': 1440, 'height': 900})
    pg.route('https://image.tmdb.org/**', serve_image)

    def shot(name):
        time.sleep(.8)
        pg.screenshot(path=os.path.join(OUT, name + '.png'))

    time.sleep(8)
    pg.goto(BASE + '#/discover'); pg.wait_for_selector('#disc .poster img'); shot('discover')
    pg.goto(BASE + '#/movie/10331'); pg.wait_for_selector('.hero h1')
    pg.click('.hero [data-act=manual]'); pg.wait_for_selector('.modal table', timeout=60000); shot('manual-search')
    pg.keyboard.press('Escape')
    time.sleep(10)  # downloads in progress
    pg.goto(BASE + '#/downloads'); pg.wait_for_selector('#dl .item'); shot('downloads')
    time.sleep(40)  # the demo downloads finish and are imported
    pg.goto(BASE + '#/movie/45745'); pg.wait_for_selector('.hero h1'); shot('movie')
    pg.goto(BASE + '#/series/33838'); pg.wait_for_selector('table.eps')
    for e in (4, 5):
        pg.click(f'[data-act=ep-check][data-k="1:{e}"]')
    shot('series')
    b.close()
print('screenshots written to', os.path.abspath(OUT))
