# Dyanis — chess engine

Шахматный движок на Go. Работает как локальный CLI, как UCI-движок
(Arena, CuteChess и т.п.) и как WebAssembly-модуль под React-доской в
браузере — без бэкенд-сервера, статичный деплой.

Техническое описание архитектуры (для разработчиков/ИИ-ассистентов) —
в [`TECHNICAL.md`](./TECHNICAL.md).

## Быстрый старт

```bash
# Поиграть в терминале (SAN: e4, Nf3, O-O — или координаты e2e4)
go run ./cmd/cli -play -depth 4

# Все тесты
go test ./...
```

```bash
# Запустить React-доску в браузере (после сборки wasm — см. ниже)
cd web && npm install && npm run dev
```

```bash
# Пересобрать wasm после правок в cmd/wasm/main.go или internal/
GOOS=js GOARCH=wasm go build -o cmd/wasm/dyanis.wasm ./cmd/wasm
```
```powershell
# То же самое в PowerShell
$env:GOOS="js"; $env:GOARCH="wasm"; go build -o cmd/wasm/dyanis.wasm ./cmd/wasm
```

## CLI

```bash
# Напечатать стартовую позицию
go run ./cmd/cli

# Напечатать произвольную позицию по FEN
go run ./cmd/cli -fen "r3k2r/p1ppqpb1/bn2pnp1/3PN3/1p2P3/2N2Q1p/PPPBBPPP/R3K2R w KQkq - 0 1"

# Perft с разбивкой по ходам
go run ./cmd/cli -perft 3

# Игра в терминале против движка (по умолчанию подключает
# assets/gm2001.bin, если файл на месте; нет файла — просто
# предупреждение, игра идёт без книги)
go run ./cmd/cli -play -depth 4

# Несколько книг цепочкой по приоритету
go run ./cmd/cli -play -depth 4 -book "assets/gm2001.bin,assets/komodo.bin"

# Явно без книги
go run ./cmd/cli -play -depth 4 -book ""

# Как UCI-движок поверх stdin/stdout
go run ./cmd/cli -uci
```

## Профилирование

```bash
go run ./cmd/profile -depth=10
go tool pprof -top cpu.prof
```

Гоняет `BestMove` от стартовой позиции под CPU-профилем и пишет
`cpu.prof` в текущую директорию. `-depth`/`-out` — опциональные флаги
(по умолчанию `depth=6`, `out=cpu.prof`). Полезно после любой правки в
`internal/board`/`internal/movegen`/`internal/eval`/`internal/search`,
которая претендует на ускорение, — проверить на реальных цифрах, а не
на глаз.

## WASM-сборка

```bash
GOOS=js GOARCH=wasm go build -o cmd/wasm/dyanis.wasm ./cmd/wasm
```

`wasm_exec.js` (копия из установки Go) уже лежит рядом в `cmd/wasm/`.
Быстрая ручная проверка, не финальный UI (подробности в
`cmd/wasm/README.md`):

```bash
python3 -m http.server 8000   # из корня репозитория
```
→ http://localhost:8000/cmd/wasm/

## React-доска (`web/`)

```bash
cd web
npm install
npm run dev
```

Перед первым запуском скопируй в `web/public/`:
- `cmd/wasm/dyanis.wasm` и `cmd/wasm/wasm_exec.js` → в `web/public/wasm/`
- книги дебютов (какие есть) → в `web/public/assets/` (браузер не
  видит файловую систему репозитория, файлы нужны в двух местах)

В интерфейсе есть поле **FEN**: «Загрузить FEN» ставит произвольную
позицию (например, чтобы разобрать конкретный момент партии на разных
глубинах), «Скопировать текущий FEN» — подтягивает FEN уже стоящей на
доске позиции в это же поле, чтобы её сохранить перед тем, как играть
дальше.

## Опционально: книга дебютов

Движок работает и без книги. Чтобы включить — скачай любой
Polyglot-совместимый `.bin` (например `gm2001.bin`) и положи в
`assets/` (и продублируй в `web/public/assets/` для браузера).