# Dyanis — техническая документация

Шахматный движок на Go. Это описание текущего устройства проекта —
без истории, кто когда что добавлял; только как это работает сейчас.
Пользовательский README (как запустить) — в [`README.md`](./README.md).

## Обзор

- `internal/` — весь движок, не публичный API (недоступен для импорта
  извне модуля).
- Три точки входа: `cmd/cli` (локальная отладка + `-play` + `-uci`),
  `internal/uci` (протокол UCI поверх stdin/stdout), `cmd/wasm`
  (JS-API `window.Dyanis` для `web/`, React-фронтенда).
- Доска — плоский массив `[64]Piece` (`internal/board`), не битборды.
  `movegen`/`search`/`eval` работают только через методы `Board`, не
  трогают `Squares` напрямую — так что смена представления в будущем
  не потребует переписывать логику поверх него.

## Структура проекта

```
dyanis-chess-engine/
├── go.mod
├── cmd/
│   ├── cli/
│   │   └── main.go              # локальный запуск: печать позиции, perft, -play (+ -book), -uci
│   └── wasm/
│       ├── main.go               # JS-API window.Dyanis: newGame/makeMove/undo/getState/
│       │                         #   engineMove/searchMove(стейтлесс, для воркера)/loadBook/...
│       │                         #   game — один живой *board.Board + стек game.moves
│       │                         #   ([]moveRecord{move, undo}) для undo, НЕ массив снимков доски
│       ├── index.html            # smoke-test страница (НЕ финальный UI, см. web/)
│       ├── README.md             # как собрать и проверить wasm-сборку
│       ├── wasm_exec.js          # копия из установки Go (глобальный класс Go)
│       └── dyanis.wasm           # собранный бинарник — В GIT НЕ КОММИТИТСЯ, см. .gitignore
├── internal/
│   ├── board/
│   │   ├── board.go, board_test.go      # Piece, Square, Board (+ приватные поля hash, kingSq),
│   │   │                                #   начальная расстановка; Copy() — для одноразовых веток
│   │   │                                #   без отката (principalVariation, notation.checkSuffix)
│   │   ├── fen.go                       # парсинг и сериализация FEN (+ начальный расчёт hash)
│   │   ├── move.go                      # Move, MakeMove(m) Undo / UnmakeMove(m, Undo) — мутируют
│   │   │                                #   доску на месте (рокировка/en passant/превращение,
│   │   │                                #   + инкрементальный hash, + кэш kingSq на ходах короля);
│   │   │                                #   симметрично MakeNullMove() NullUndo / UnmakeNullMove
│   │   └── zobrist.go, zobrist_test.go  # Board.Hash() — просто читает поле;
│   │                                    #   computeHashFromScratch/VerifyHash — для инициализации и тестов
│   ├── book/
│   │   └── book.go, book_test.go        # Load (с диска) / Parse (из байт, для браузера) Polyglot .bin,
│   │                                    #   Chain/Source: несколько книг цепочкой по приоритету
│   ├── eval/
│   │   ├── bishops.go            # бонус за пару слонов
│   │   ├── eval.go, eval_test.go # материал + вызов всех остальных слагаемых, gamePhase
│   │   ├── king_safety.go        # пешечный щит + открытые линии рядом с королём
│   │   ├── mobility.go           # подвижность фигур (безопасные клетки хода)
│   │   ├── mopup.go              # "загони голого короля к краю" в выигранных эндшпилях
│   │   ├── pawns.go              # сдвоенные/изолированные/проходные пешки
│   │   └── pst.go                # piece-square таблицы (Michniewski), taper короля по фазе
│   ├── movegen/
│   │   ├── movegen.go, movegen_test.go   # генерация ходов + определение атакованных полей
│   │   ├── notation.go, notation_test.go # SAN: рендер и разбор ходов (Nf3, exd5, O-O, e8=Q#), GameLog
│   │   └── status.go, status_test.go     # GameStatus/GameStatusFromMoves (мат/пат/50-move rule/
│   │                                     #   insufficient material) + GameStatusWithHistory
│   │                                     #   (+ троекратное повторение, нужна история партии)
│   ├── perft/
│   │   └── perft.go, perft_test.go      # Perft()/Divide()/VerifyHashes() — эталонные числа: старт + Kiwipete
│   ├── search/
│   │   ├── opening_book.go, opening_book_test.go # BestMoveWithBook(): книга — приоритет перед поиском
│   │   ├── search.go, search_test.go    # negamax + alpha-beta + TT + quiescence + null-move pruning
│   │   │                                #   + move ordering (TT-ход/MVV-LVA/killers/history)
│   │   │                                #   + LMR/PVS/aspiration windows + SearchInfo (для UCI info-строк)
│   │   ├── see.go                       # Static Exchange Evaluation, используется quiescence-ом
│   │   ├── info_test.go                 # SearchInfo/PV/mate-score/node-count тесты
│   │   └── tt_test.go                   # тесты транспозиционной таблицы
│   └── uci/
│       └── uci.go, uci_test.go          # UCI-протокол: uci/isready/position/go/quit,
│                                        #   стримит info depth/score/nodes/pv на каждой глубине
├── assets/                      # book.bin'ы (gm2001.bin, komodo.bin, performance.bin, ...) —
│                                 #   читает CLI/UCI с диска; для браузера копия нужна отдельно
│                                 #   в web/public/assets/ (см. ниже)
└── web/                         # React-доска
    ├── package.json, vite.config.js, index.html
    ├── public/
    │   ├── wasm/                # dyanis.wasm + wasm_exec.js — копируются сюда вручную
    │   ├── assets/               # book.bin'ы — копируются сюда вручную
    │   └── engine-worker.js     # отдельный Web Worker: своя копия wasm, только для поиска
    └── src/
        ├── main.jsx, App.jsx, App.css, index.css
        ├── engine/
        │   └── dyanisEngine.js  # обёртка: sync API основного потока (бухгалтерия партии) +
        │                        #   async API воркера (сам поиск, никогда не блокирует страницу)
        ├── hooks/
        │   └── useElementWidth.js
        └── components/
            ├── Board.jsx        # react-chessboard: drag&drop + клик-клик
            └── MoveLog.jsx
```

## `internal/board`

| Файл | Содержимое |
|---|---|
| `board.go` | `Piece`, `Color`, `Square`, `CastlingRights`, `Board`. `Board.Copy()` — глубокая копия. |
| `move.go` | `Move`, `MakeMove`/`UnmakeMove`, `MakeNullMove`/`UnmakeNullMove`. |
| `zobrist.go` | Polyglot-совместимый Zobrist-хеш. `Board.Hash()`, `VerifyHash()`. |
| `fen.go` | `FromFEN`/`ToFEN`, `StartFEN`. |

**`Board.KingSquare(c)` кэширует позицию короля**, а не сканирует
все 64 клетки на каждый вызов — профилирование показало, что это
реальная нагрузка: `KingSquare` вызывается на каждого псевдо-легального
кандидата внутри `GenerateLegalMoves`' king-safety фильтра, то есть
очень часто. Кэш (`Board.kingSq`) поддерживается в `MakeMove`/
`UnmakeMove` при ходе королём (включая обе рокировки — `Move.To` там
уже нормализованное поле назначения короля, не Polyglot-кодировка
"король берёт ладью"). Кэш не считается источником истины: перед
использованием `KingSquare` сверяет, что клетка из кэша ДЕЙСТВИТЕЛЬНО
всё ещё держит короля нужного цвета, и если нет (например, `Board`
собран напрямую через `&Board{...}`, минуя `NewInitialBoard`/`FromFEN`,
как делают некоторые тестовые хелперы) — откатывается на честный
full-scan и сам чинит кэш для следующих вызовов. Так что это чисто
оптимизация производительности, а не требование к тому, как именно
собран `Board`.

**Move application — make/unmake, не копирование.** `MakeMove(m) Undo`
мутирует получателя на месте и возвращает `Undo` — снимок всего, что
нужно откатить (сдвинутая фигура; взятая фигура и клетка, откуда её
сняли — не совпадает с `To` при взятии на проходе; старые castling
rights/en passant/halfmove/fullmove/hash). `UnmakeMove(m, undo)`
восстанавливает эти поля прямым присваиванием — не разворотом
XOR-цепочки хеша задом наперёд, так что откат корректен независимо от
внутренней реализации инкрементального хеша. Симметрично для
`MakeNullMove() NullUndo` / `UnmakeNullMove(NullUndo)`.

Стандартная дисциплина вызова — как в стеке рекурсии:
```go
undo := b.MakeMove(m)
// ... смотрим на позицию, рекурсируем ...
b.UnmakeMove(m, undo)
```

**`Board.Copy()` остаётся в API** для мест, где ветка одноразовая и
её нечем/незачем откатывать, а исходную (часто ЧУЖУЮ, живую) позицию
трогать нельзя: `search.principalVariation` (строит PV чтением TT, не
владеет переданной ей доской) и `movegen.checkSuffix` в `notation.go`
(тот же случай). Любой код, который держит указатель на позицию "до
хода" и ожидает, что `MakeMove` где-то ещё её не тронет, обязан явно
взять `Copy()` заранее — само по себе это не гарантируется.

**Zobrist-хеш инкрементальный.** `Board.Hash()` — просто чтение поля;
`MakeMove`/`MakeNullMove` точечно XOR'ят только изменившееся (фигуры,
отозванные права на рокировку, старый/новый en passant, сторона хода).
Полный пересчёт (`computeHashFromScratch`) — только там, где хеш
неоткуда взять инкрементально: `NewInitialBoard`/`FromFEN`.
`VerifyHash()` сверяет поле с полным пересчётом — полезно гонять по
perft-дереву при любых правках `MakeMove`/`UnmakeMove`, трогающих хеш.

## `internal/movegen`

| Файл | Содержимое |
|---|---|
| `movegen.go` | Генерация ходов: псевдо-легальные → фильтр по "не подставляет своего короля" (make → проверка → unmake). `IsSquareAttacked`. |
| `notation.go` | SAN: `SAN`/`ParseSAN`, `GameLog`. |
| `status.go` | `GameStatus`, `GameStatusWithHistory`, `InCheck`, `InsufficientMaterial`. |

`GenerateLegalMoves` строит псевдо-легальные ходы, затем на каждый
делает `MakeMove` → проверяет, не под ударом ли свой король → откатывает.

`GameStatus` — мат/пат/50-move rule/insufficient material,
самодостаточна (не нужна история партии). `InsufficientMaterial` —
намеренно упрощённое правило: K vs K, K+лёгкая vs K, K+B vs K+B с
одноцветными слонами; не полная dead-position детекция.

`GameStatusWithHistory` добавляет троекратное повторение — этому
принципиально нужна история позиций, которой у одиночного `*Board`
нет. Живёт на уровне игровых циклов (CLI/`-play`, wasm, UCI), НЕ
внутри рекурсии `negamax`/`quiescenceSearch` — там нет протаскиваемой
истории позиций. Конечное по глубине дерево поиска не зацикливается
так, как бесконечная реальная партия, так что это не создаёт ошибку —
просто иногда чуть менее точная оценка линий, транспонирующихся в уже
виденную позицию. 50-move rule, в отличие от repetition, детектится
прямо внутри `negamax`/`quiescenceSearch` через `board.HalfmoveClock`
(самодостаточное поле).

CLI и wasm сами ведут партию от начала до конца и наращивают историю
инкрементально на каждый ход. UCI устроен иначе: GUI при каждой
`position ...` присылает ПОЛНЫЙ список ходов от `startpos`/`fen`, а не
только новые — `session.history` в `uci.go` каждый раз пересобирается
с нуля вместе с позицией.

## `internal/search`

| Файл | Содержимое |
|---|---|
| `search.go` | `negamax`, alpha-beta, TT, quiescence, move ordering, `BestMove*`/`*Info`-варианты, `SearchInfo`. |
| `see.go` | Static Exchange Evaluation. |
| `opening_book.go` | `BestMoveWithBook*` — книга как приоритет перед поиском. |

**Move loop** делает `undo := b.MakeMove(m)` → все под-вызовы (первое
полное окно / PVS-пробники / LMR) работают на этой же, уже сделанной
позиции `b`, без отдельного `child` на каждую попытку → `b.UnmakeMove(m, undo)`
в конце итерации. Null-move pruning — та же пара с `MakeNullMove`/
`UnmakeNullMove`.

**Mate-distance adjustment.** `negamax` знает `ply` (глубину от корня,
+1 на каждый рекурсивный вызов, включая null-move пробу) и возвращает
`ply - MateScore`, а не плоский `-MateScore` — иначе мат в 1 ход и мат
в 5 ходов давали бы одинаковый счёт. TT хранит mate-score в
"node-relative" виде через `storeAdjustMateScore`/`readAdjustMateScore`,
конвертируя под текущий `ply` при чтении — без этого закешированное
значение с одной дистанции до мата могло бы тихо подставиться на
другую дистанцию при транспозиции в ту же позицию другим путём.

**Move ordering:** TT-ход → взятия по MVV-LVA → killer moves (тихие
ходы, давшие отсечение на этой же глубине в соседней позиции) → history
heuristic.

**Null-move pruning** — с защитой от цугцванга (`hasNonPawnMaterial`)
и от срабатывания в корне (`isRoot` у `negamax`).

**LMR встроен внутрь PVS**, не отдельной веткой. Первый ход в списке
(тот, на который уже поставило ordering) — сразу полное окно
`(-beta, -alpha)`. Каждый следующий — сначала дешёвая проба нулевым
окном `(-alpha-1, -alpha)`; для поздних тихих ходов (после
`lmrFullDepthMoves`-го, на глубине `>= lmrMinDepth`, не взятие/
превращение, не под шахом) первая такая проба идёт на уменьшенной
глубине. Если проба выбивает `alpha` — подтверждающая проба на полной
глубине тем же нулевым окном; если и она проходит — дорогой полный
пересчёт `(-beta, -alpha)`.

**Aspiration windows.** `BestMoveTimedInfo` глубже `aspirationMinDepth`
стартует не с `(-Infinity, Infinity)`, а с узкого окна вокруг счёта
предыдущей глубины ± `aspirationDelta`; попадание счёта точно на
границу окна — непроверенный bound, та же глубина пересчитывается
полным окном.

**SEE** (`see.go`) работает на копии `board.Squares` (не битборды),
прогрессивно "съедая" фигуры из локального массива — рентген-атаки
вскрываются сами при повторном сканировании клетки, без отдельного
x-ray-трюка. Используется в `quiescenceSearch` (отсечение взятий с
`SEE < 0`, сортировка оставшихся по настоящему SEE); в ordering'е
`negamax` по всему дереву НЕ используется — там MVV-LVA (SEE на каждый
узел всего дерева был бы заметно дороже, чем в quiescence).

**`quiescenceSearch`** досматривает взятия (и, с ограниченным
бюджетом, шаховые серии) за пределами номинальной глубины — иначе
негамакс мог бы остановиться прямо посреди размена.

**`SearchInfo`/`...Info`-варианты.** `BestMoveInfo`, `BestMoveTimedInfo`,
`BestMoveWithBookInfo`, `BestMoveTimedWithBookInfo` принимают колбэк
`onInfo func(SearchInfo)`, вызываемый на каждую завершённую глубину
(глубина, счёт, PV, счётчик узлов). `BestMove`/`BestMoveTimed`/
`BestMoveWithBook`/`BestMoveTimedWithBook` — тонкие обёртки с
`onInfo = nil`. Счётчик узлов (`TranspositionTable.nodes`) не
сбрасывается между глубинами одного вызова — накопительный итог за
весь поиск. PV не хранится отдельной triangular-таблицей внутри
рекурсии, а восстанавливается после каждой глубины проходом по уже
заполненной TT от корня (`principalVariation`, с защитой от коллизии
хеша и от цикла).

**Книга дебютов проверяется до поиска, не внутри eval.** Вес хода в
книге не несёт смысла как статичная оценка произвольной позиции
глубоко в дереве — поэтому это короткое замыкание в самом начале
`BestMoveWithBook*`, а не слагаемое в `eval.Evaluate`. `bk book.Source`
может быть `nil` (без книги), `*book.Book` или `*book.Chain`
(несколько книг по приоритету — см. ниже).

## `internal/book`

Polyglot `.bin`: `Load` (с диска) / `Parse` (из байт — для wasm/браузера,
там нет файловой системы). `Entry{Key, Move, Weight, Learn}`.
`DecodeMove` разбирает 16-битный упакованный ход и сопоставляет с
переданным списком легальных ходов (включая нормализацию Polyglot'овской
кодировки рокировки "король берёт свою ладью").

`Source` — интерфейс (`LookupBoard`), которому удовлетворяют и `*Book`,
и `*Chain`. **Несколько книг подключаются цепочкой, не слиянием
записей**: `Entry.Weight` — относительная частота *внутри одной
конкретной книги*, не нормализованная вероятность; смешивание весов
разных книг фаворитизировало бы книгу с бо́льшими числами без всякого
шахматного смысла. `Chain.LookupBoard` пробует книги по порядку
приоритета, возвращает записи первой, где для позиции что-то нашлось.

## `internal/eval`

Материал + piece-square tables (`pst.go`, тейпинг midgame/endgame по
фазе — только для короля) + mobility + пешечная структура (сдвоенные/
изолированные/проходные) + king safety (пешечный щит, открытые линии,
тейпинг по фазе) + пара слонов + mop-up (`mopup.go` — при решающем
материальном перевесе тянет слабого короля к краю, а своего — на
помощь: без этого остальные слагаемые насыщаются и движок может
бесконечно "шаркать" фигурами вместо доведения мата).

## `internal/perft`

`Perft(b, depth)`/`Divide(b, depth)` — брутфорс-подсчёт листьев дерева
легальных ходов, через тот же цикл `MakeMove`→рекурсия→`UnmakeMove`.
`VerifyHashes` идёт по тому же дереву и на каждом узле сверяет
`VerifyHash()`. Эталонные числа — стартовая позиция и Kiwipete.

## `internal/uci`

Протокол: `uci`/`isready`/`ucinewgame`/`position [startpos|fen ...] [moves ...]`/
`go [depth N] [movetime N]`/`quit`. `go` без `depth` — фиксированная
глубина, заданная `Loop`; `movetime` включает итеративное углубление
с этой глубиной как потолком. `handleGo` подписывается на `onInfo` и
пишет `info depth D score (cp N|mate N) nodes N pv ...` перед
`bestmove`; ход из книги — без `info`-строки (поиска не было).
`wtime`/`btime`/`winc`/`binc`/`infinite` принимаются, но игнорируются.
Неизвестные команды молча игнорируются (по спеку UCI).

## `cmd/cli`

Флаги: `-fen`, `-perft`, `-play`, `-uci`, `-depth`, `-movetime`,
`-book` (список путей через запятую, приоритет по порядку; путь,
который не загрузился, — предупреждение и пропуск, не хардфейл).
`-play` — интерактивная партия в терминале: ходы SAN или координаты,
лог партии в стандартной нотации по завершении.

## `cmd/wasm`

`//go:build js && wasm`. JS-API `window.Dyanis` (см. `registerAPI`):
`newGame`/`getState`/`makeMove`/`undo`/`engineMove`/`perft`/`loadBook`/
`clearBook`/`bookInfo`/`searchMove`. Каждая функция возвращает JSON.

**Состояние партии — один живой `*board.Board` (`game.pos`), не массив
снимков.** Каждый сыгранный полуход мутирует `game.pos` через
`b.MakeMove(m)` и кладётся в `game.moves []moveRecord{move, undo}` —
стек, который `Dyanis.undo()` разматывает вызовом
`game.pos.UnmakeMove(rec.move, rec.undo)`. Хеши для
`GameStatusWithHistory` держатся отдельным параллельным
`game.hashes []uint64` (взять их больше неоткуда — отдельных объектов
доски на каждую позицию не существует).

**Два независимых экземпляра wasm-модуля**: один в основном потоке
страницы (бухгалтерия партии — ходы, история, undo; никогда не
блокирует), второй в отдельном Web Worker (только поиск, изолирован по
памяти от первого). `Dyanis.searchMove(fen, depth, movetimeMs)` —
стейтлесс-вариант специально для воркера: он не читает и не пишет
`game`, воркеру неоткуда взять текущую позицию иначе, кроме явного
параметра. Решённый воркером ход только предлагается; применяет его к
реальному состоянию партии всегда основной поток, тем же
`Dyanis.makeMove`, что и для хода человека.

`Dyanis.loadBook(bytes)` принимает `Uint8Array` с сырым содержимым
`.bin` (в браузере нет файловой системы, JS сам делает `fetch()` и
передаёт байты); можно вызвать несколько раз подряд — книги
добавляются в цепочку по приоритету (см. `internal/book`).
`Dyanis.clearBook()` сбрасывает цепочку.

## `web/`

React-доска (`react-chessboard`, drag&drop + клик-клик). Поиск ходов —
в `web/public/engine-worker.js` (отдельный Web Worker со своей копией
wasm-модуля, см. выше). Обёртка над обоими инстансами — 
`web/src/engine/dyanisEngine.js`.