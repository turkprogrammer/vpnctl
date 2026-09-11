# Changelog

Все заметные изменения проекта фиксируются в этом файле.

Формат основан на [Keep a Changelog](https://keepachangelog.com/ru/1.1.0/),
проект придерживается [Semantic Versioning](https://semver.org/lang/ru/spec/v2.0.0.html).

## [Unreleased]

### Added

- Тесты для `internal/conf`, `internal/priv`, `internal/vpn`: table-driven
  кейсы с детектором гонок, фаззинг-цели `FuzzInterfaceName` и
  `FuzzValidate`, покрытие ~97.9 / 87.5 / 95.5 %.
- Переменные-швы `priv.Command` (подмена вызовов `sudo`/`pkexec`) и
  `vpn.netClassDir` (подмена `/sys/class/net`) для безопасных тестов без
  привилегий.
- Конфигурация линтера `.golangci.yml` (errcheck, govet, staticcheck, unused,
  ineffassign, unconvert, gofmt).
- Раздел «Тестирование» в `README.md`: `-race`, фаззинг, покрытие, линт.

### Changed

- `vpn.Up` принимает `context.Context` первым аргументом; отмена контекста
  прерывает ожидание интернета и выполняет откат туннеля.
- Сбой отката (`awg-quick down`) больше не проглатывается: `upRollback`
  сообщает о нём в тексте ошибки.
- Один общий `ipClient` для проверки внешнего IP вместо нового
  `http.Client`/`Transport` на каждый запрос; `fetchIP` обрабатывает ошибку
  чтения ответа.
- `conf.Import` переиспользует имя интерфейса из `Validate` вместо повторного
  вычисления через `InterfaceName`; в `conf.InterfaceName` удалена мёртвая
  ветка верхнего регистра (после `strings.ToLower` недостижима).
- Текст справки CLI указывает синонимы `tray` / `ui` / `gui`.

### Fixed

- Справка CLI: «графический интерфейс (окно)» вместо ложного «окно + трей».
- Опечатка в сообщении об откате: «откатено» → «откачено».

## [0.1.0] - 2026-09-11

### Added

- CLI-команды `status`, `up`, `down`, `import` (синонимы `tray` / `ui` / `gui`
  для интерфейса).
- Безопасный подъём VPN: `awg-quick up`, ожидание появления интернета через
  туннель до 20 с и автоматический откат при неудаче; уже поднятый интерфейс
  не пересоздаётся.
- Импорт `.conf` одним привилегированным вызовом (guard → `mkdir -p` →
  `install 0600` → атомарный `mv` → очистка): один запрос пароля на импорт,
  существующий конфиг не перезаписывается (`ErrExists`).
- Проверка внешнего IP по нескольким хостам, каждый запрос — новым соединением
  (`VPNCTL_IP_URL` — первый по приоритету).
- Статус без прав: состояние интерфейса через `/sys/class/net` + внешний IP;
  Endpoint читается через `awg-quick strip` без окна пароля.
- Единая точка повышения привилегий `internal/priv` (`Run` — pkexec,
  `RunWG` — `sudo -n` с фолбэком на pkexec, `TryWG` — без запроса).
- GTK3-окно: тёмная терминальная тема, форма загрузки конфига,
  кнопка «ВКЛЮЧИТЬ/ВЫКЛЮЧИТЬ VPN», мониторинг статуса каждые 3 секунды.
- Иконки приложения в `assets/icons/hicolor/`.

### Removed

- Неиспользуемый тип `priv.Result` и поле `App.win`.

[Unreleased]: https://github.com/turkprogrammer/vpnctl/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/turkprogrammer/vpnctl/releases/tag/v0.1.0