// Camera panel, mounted by the Dreame Local web UI: mount(element, ctx).
export default function mount(el, ctx) {
  el.innerHTML = `<label class="check"><input data-r="enabled" type="checkbox">Показывать снимки камеры</label>
<p class="muted small">Во время уборки прошивка сама фотографирует фронтальной камерой и складывает несколько последних снимков на робот (для сбора данных ИИ). Модуль только показывает их здесь: снимки не покидают робот через модуль. Выключено — модуль снимки не отдаёт.</p>
<p data-r="status" class="muted small" role="status"></p>
<div class="row"><select data-r="list" aria-label="Снимок"></select><button data-r="refresh">Обновить</button></div>
<img data-r="photo" alt="Снимок камеры робота" hidden style="max-width:100%;border-radius:8px;margin-top:8px">`;
  const $ = name => el.querySelector(`[data-r="${name}"]`);
  const time = s => new Date(s * 1000).toLocaleString();
  let active = false, shown = '', last = 0;

  async function show(name) {
    try {
      const p = await ctx.call('photo', name ? { name } : {});
      $('photo').src = 'data:image/jpeg;base64,' + p.jpeg; $('photo').hidden = false; shown = p.name;
    } catch (e) { $('photo').hidden = true; $('status').textContent = e.message; }
  }
  function render(s) {
    $('enabled').checked = s.enabled;
    $('list').innerHTML = s.photos.map(p => `<option value="${p.name}">${time(p.at)}</option>`).join('');
    $('list').disabled = $('refresh').disabled = !s.enabled;
    $('status').textContent = !s.folder ? 'Папки со снимками ещё нет: она появится после первой уборки.'
      : s.photos.length ? 'Снимков: ' + s.photos.length + ', последний ' + time(s.photos[0].at) : 'Снимков нет: прошивка удаляет их после отправки или новой уборки.';
    if (!s.enabled) { $('photo').hidden = true; shown = ''; return; }
    if (s.photos.length && s.photos[0].name !== shown && (!shown || $('list').selectedIndex <= 0)) show(s.photos[0].name);
  }
  async function refresh() { try { render(await ctx.read('status')); } catch (e) { $('status').textContent = e.message; } }

  $('enabled').onchange = async () => {
    try { render(await ctx.call('set', { enabled: $('enabled').checked })); } catch (e) { ctx.toast(e.message, true); refresh(); }
  };
  $('list').onchange = () => show($('list').value);
  $('refresh').onclick = refresh;
  // New photos appear only while the robot cleans: follow them then.
  ctx.onState(s => {
    const cleaning = s.activity === 'cleaning';
    if (cleaning && !document.hidden && Date.now() - last > 5000) { last = Date.now(); refresh(); }
    else if (active && !cleaning) refresh();
    active = cleaning;
  });
  refresh();
}
