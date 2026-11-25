// Camera panel, mounted by the Dreame Local web UI: mount(element, ctx).
export default function mount(el, ctx) {
  el.innerHTML = `<label class="check"><input data-r="enabled" type="checkbox">Показывать снимки из уборок</label>
<p class="muted small">Во время уборки прошивка сама фотографирует фронтальной камерой и складывает несколько последних снимков на робот (для сбора данных ИИ). Модуль только показывает их здесь: снимки не покидают робот через модуль. Выключено — модуль снимки не отдаёт. Живой просмотр от этой галочки не зависит.</p>
<label class="check"><input data-r="light" type="checkbox">Подсветка при просмотре</label>
<p class="muted small">Смотреть камеру и ездить — кнопка камеры на карте агента. «Подсветка при просмотре» включает фронтальный фонарь робота, пока камера снимает (там или в Home Assistant).</p>
<p data-r="status" class="muted small" role="status"></p>
<div class="row"><select data-r="list" aria-label="Снимок"></select><button data-r="refresh">Обновить</button></div>
<img data-r="photo" alt="Снимок камеры робота" hidden>
<details data-r="code-box"><summary>Код доступа к видео в приложении Dreame</summary>
<p data-r="code-status" class="muted small" role="status"></p>
<form data-r="code-form"><label data-r="code-old-label">Текущий код<input data-r="code-old" type="password" inputmode="numeric" pattern="[0-9]{4}" maxlength="4" autocomplete="off"></label>
<label>Новый код<input data-r="code-new" type="password" inputmode="numeric" pattern="[0-9]{4}" maxlength="4" autocomplete="off" required></label><button type="submit">Задать код</button></form>
<button data-r="code-reset">Сбросить код кнопкой «Домой»</button>
<p class="muted small">Код из 4 цифр спрашивает видеонаблюдение в приложении Dreame. Агенту 0.71+ он тоже нужен для ручного управления (панель агента → Камера): с ним робот ездит в режиме видеомониторинга, без мотора пылесоса. Просмотр здесь его не спрашивает. Забыл код — нажми «Сбросить» и в течение 5 минут нажми «Домой» на роботе три раза подряд: робот удалит код и скажет об этом.</p></details>`;
  const $ = name => el.querySelector(`[data-r="${name}"]`);
  // The agent's CSP drops style attributes in panel HTML; styles set from script apply.
  for (const name of ['photo']) Object.assign($(name).style, { maxWidth: '100%', borderRadius: '8px', marginTop: '8px' });
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
    $('light').checked = s.light;
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
  $('light').onchange = async () => {
    try { render(await ctx.call('light', { on: $('light').checked })); } catch (e) { ctx.toast(e.message, true); refresh(); }
  };
  // Video access code: while allowed, three presses of Home on the robot clear it.
  let codeWaiting = false, codePoll = 0;
  async function code(params) {
    clearTimeout(codePoll);
    const c = await ctx.call(params.code ? 'code_set' : 'code', params);
    codeWaiting = c.waiting;
    $('code-old-label').hidden = !c.code_set; $('code-old').required = c.code_set;
    $('code-status').textContent = c.waiting ? 'Жду: нажми «Домой» на роботе три раза подряд.' : c.reset ? 'Код сброшен кнопкой «Домой».' : c.code_set ? 'Код задан.' : 'Код не задан.';
    $('code-reset').textContent = c.waiting ? 'Отменить' : 'Сбросить код кнопкой «Домой»';
    if (c.reset) ctx.toast('Код доступа к видео сброшен');
    if (c.waiting && el.isConnected) codePoll = setTimeout(() => code({}).catch(e => ctx.toast(e.message, true)), 3000);
  }
  $('code-box').ontoggle = () => { if ($('code-box').open) code({}).catch(e => { $('code-status').textContent = e.message; }); };
  $('code-reset').onclick = () => code({ enabled: !codeWaiting }).catch(e => ctx.toast(e.message, true));
  $('code-form').onsubmit = e => {
    e.preventDefault();
    const old = $('code-old'), neu = $('code-new');
    code({ code: neu.value, ...(old.required ? { old_code: old.value } : {}) })
      .then(() => { old.value = neu.value = ''; ctx.toast('Код доступа к видео задан'); }, e => ctx.toast(e.message, true));
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
