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
- **Доска — битборды, не плоский массив.** `internal/board.Board`
  держит per-(тип фигуры, цвет) `Bitboard`-а как источник истины для
  генерации ходов/атак, плюс mailbox-массив `squares [64]Piece`
  (приватный) как вторичный, синхронно поддерживаемый кэш — нужен
  только для O(1) чтения "что стоит на этой клетке" (`PieceAt`) и для
  кода, которому реально нужен снимок всей доски разом (`SquaresArray`,
  SEE). `movegen`/`search`/`eval` работают только через методы `Board`
  (`Pieces`, `Occupied`, `OccupiedBy`, `PieceAt`, `SquaresArray`,
  `SetSquare`), не трогают ни `squares`, ни битборды напрямую.
- Слайдеры (слон/ладья/ферзь) — magic bitboards, O(1) lookup вместо
  ray-walk. Не-слайдеры (пешка/конь/король) — precomputed таблицы атак.
- Генерация ходов **напрямую легальная** (pin-mask/checkers-mask), не
  generate-then-filter через make/unmake — кроме одного намеренного
  исключения (взятие на проходе, вскрывающее шах по горизонтали).

## Структура проекта

```
dyanis-chess-engine/
├── go.mod
├── cmd/
│   ├── cli/
│   │   └── main.go              # локальный запуск: печать позиции, perft, -play (+ -book), -uci
│   ├── wasm/
│   │   ├── main.go               # JS-API window.Dyanis: newGame/makeMove/undo/getState/
│   │   │                         #   engineMove/searchMove(стейтлесс, для воркера)/loadBook/...
│   │   │                         #   game — один живой *board.Board + стек game.moves
│   │   │                         #   ([]moveRecord{move, undo}) для undo, НЕ массив снимков доски
│   │   ├── index.html            # smoke-test страница (НЕ финальный UI, см. web/)
│   │   ├── README.md             # как собрать и проверить wasm-сборку
│   │   ├── wasm_exec.js          # копия из установки Go (глобальный класс Go)
│   │   └── dyanis.wasm           # собранный бинарник — В GIT НЕ КОММИТИТСЯ, см. .gitignore
│   └── profile/
│       └── main.go               # go run ./cmd/profile [-depth=N] [-out=cpu.prof] — CPU-профиль
│                                  #   BestMove(depth) от старта; go tool pprof -top cpu.prof дальше
├── internal/
│   ├── board/
│   │   ├── board.go, board_test.go  # Piece, Square, Board (+ приватные поля: squares [64]Piece
│   │   │                            #   mailbox, bb bitboards, hash uint64, kingSq [2]Square,
│   │   │                            #   materialPST int), начальная расстановка; Copy() — для
│   │   │                            #   одноразовых веток без отката
│   │   ├── bitboard.go              # Bitboard, per-(тип,цвет) битборды, precomputed knight/king/
│   │   │                            #   pawn attack tables, Pieces/Occupied/OccupiedBy/PieceAt/
│   │   │                            #   SquaresArray/SetSquare
│   │   ├── magic.go                 # magic bitboards: RookAttacks/BishopAttacks/QueenAttacks —
│   │   │                            #   O(1) слайдер-атаки вместо ray-walk (числа найдены офлайн-
│   │   │                            #   поиском, независимо перепроверены полным перебором occupancy)
│   │   ├── lines.go                 # SquaresBetween[64][64] — клетки строго между двумя данными,
│   │   │                            #   для связок и (будущей) генерации блокировки шаха
│   │   ├── material_hook.go         # PieceValue-хук (регистрируется eval'ом) + инкрементальное
│   │   │                            #   сопровождение Board.materialPST в MakeMove/UnmakeMove/SetSquare
│   │   ├── fen.go                   # парсинг и сериализация FEN (+ начальный расчёт hash/bb/materialPST)
│   │   ├── move.go                  # Move, MakeMove(m) Undo / UnmakeMove(m, Undo) — мутируют
│   │   │                            #   доску на месте (рокировка/en passant/превращение,
│   │   │                            #   + инкрементальные hash/bb/materialPST, + кэш kingSq);
│   │   │                            #   симметрично MakeNullMove() NullUndo / UnmakeNullMove
│   │   └── zobrist.go, zobrist_test.go  # Board.Hash() — просто читает поле;
│   │                                    #   computeHashFromScratch/VerifyHash — для инициализации и тестов
│   ├── book/
│   │   └── book.go, book_test.go        # Load (с диска) / Parse (из байт, для браузера) Polyglot .bin,
│   │                                    #   Chain/Source: несколько книг цепочкой по приоритету
│   ├── eval/
│   │   ├── bishops.go            # бонус за пару слонов — Bitboard.Count() по слонам, без скана
│   │   ├── eval.go, eval_test.go, eval_material_verify_test.go
│   │   │                         # материал + вызов всех остальных слагаемых, gamePhase;
│   │   │                         #   verifyMaterialPST — сверка инкремента с нуля, гоняется по
│   │   │                         #   perft-дереву (живёт здесь, не в internal/perft — хук
│   │   │                         #   регистрируется только этим пакетом)
│   │   ├── king_safety.go        # пешечный щит + открытые линии рядом с королём, битборды
│   │   ├── mobility.go           # подвижность фигур — attack-битборд минус свои минус под пешкой
│   │   ├── mopup.go              # "загони голого короля к краю" в выигранных эндшпилях
│   │   ├── pawn_cache.go         # PawnCache — per-поиск кэш pawnStructureScore по битбордам пешек
│   │   ├── pawns.go              # сдвоенные/изолированные/проходные пешки, fileMask/passedMask
│   │   └── pst.go                # piece-square таблицы (Michniewski), taper короля по фазе;
│   │                              #   init() регистрирует board.PieceValue-хук
│   ├── movegen/
│   │   ├── movegen.go             # GenerateLegalMoves — напрямую легальная генерация через
│   │   │                          #   Checkers()/Pins() (checkmask/pinmask), без make/unmake в
│   │   │                          #   цикле легальности, кроме взятия на проходе (см. ниже);
│   │   │                          #   GenerateLegalCaptures — только взятия, для quiescence;
│   │   │                          #   IsSquareAttacked/AttackersTo/Checkers/Pins;
│   │   │                          #   generateLegalMovesSlow — старый make/unmake-генератор,
│   │   │                          #   временно оставлен как эталон для сверки, кандидат на удаление
│   │   ├── movegen_verify_test.go # VerifyLegalMoveGeneration — сверка нового генератора со
│   │   │                          #   старым как МНОЖЕСТВ ходов (не только счётчиков) по всему дереву
│   │   ├── pins_fuzz_test.go      # случайные позиции (ровно один король на цвет) — Pins() не
│   │   │                          #   должен разрешать то, что GenerateLegalMoves запрещает
│   │   ├── notation.go, notation_test.go # SAN: рендер и разбор ходов (Nf3, exd5, O-O, e8=Q#), GameLog
│   │   └── status.go, status_test.go     # GameStatus/GameStatusFromMoves (мат/пат/50-move rule/
│   │                                     #   insufficient material) + GameStatusWithHistory
│   │                                     #   (+ троекратное повторение, нужна история партии)
│   ├── perft/
│   │   └── perft.go, perft_test.go  # Perft()/Divide()/VerifyHashes()/VerifyBitboards() —
│   │                                #   эталонные числа: старт + Kiwipete
│   ├── search/
│   │   ├── opening_book.go, opening_book_test.go # BestMoveWithBook(): книга — приоритет перед поиском
│   │   ├── search.go, search_test.go, tt_test.go
│   │   │       # negamax (+ check extensions, + futility pruning) + alpha-beta +
│   │   │       #   TranspositionTable (фиксированный массив, не map — depth-preferred
│   │   │       #   замещение) + quiescence (captures-only генерация, не in check) +
│   │   │       #   null-move pruning + move ordering (TT-ход/MVV-LVA/killers/history,
│   │   │       #   history — плоский массив [64*64]int, не map) + LMR/PVS/aspiration
│   │   │       #   windows + SearchInfo (для UCI info-строк)
│   │   ├── see.go                       # SEE на битбордах (seeBoard — локальный мутируемый
│   │   │                                #   снимок [цвет][тип]Bitboard, не копия [64]Piece)
│   │   ├── info_test.go                 # SearchInfo/PV/mate-score/node-count тесты
│   ├── uci/
│   │   └── uci.go, uci_test.go          # UCI-протокол: uci/isready/position/go/quit,
│   │                                    #   стримит info depth/score/nodes/pv на каждой глубине
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
| `board.go` | `Piece`, `Color`, `Square`, `CastlingRights`, `Board`. `Board.Copy()` — глубокая копия (все поля — плоские значения, копируются вместе с `Board` автоматически). |
| `bitboard.go` | `Bitboard` (тип-обёртка над `uint64`), 12 битбордов по (тип, цвет), таблицы атак коня/короля/пешки, `Pieces`/`Occupied`/`OccupiedBy`/`PieceAt`/`SquaresArray`/`SetSquare`. |
| `magic.go` | Magic bitboards для слайдеров: `RookAttacks`/`BishopAttacks`/`QueenAttacks`. |
| `lines.go` | `SquaresBetween(a, b)` — клетки строго между `a` и `b`, если они на одной линии/диагонали. |
| `material_hook.go` | `PieceValue`-хук + `Board.materialPST` (инкрементальная сумма материал+PST по всем фигурам кроме короля). |
| `move.go` | `Move`, `MakeMove`/`UnmakeMove`, `MakeNullMove`/`UnmakeNullMove`. |
| `zobrist.go` | Polyglot-совместимый Zobrist-хеш. `Board.Hash()`, `VerifyHash()`. |
| `fen.go` | `FromFEN`/`ToFEN`, `StartFEN`. |

### Представление: битборды + mailbox

`Board` хранит:
- `bb` — 12 приватных `Bitboard` (по типу фигуры × цвету) + occupancy-битборды — источник истины для генерации ходов, атак, связок.
- `squares [64]Piece` — приватный mailbox-массив, синхронно поддерживаемый вместе с `bb` в каждой точке, где меняется доска (`MakeMove`/`UnmakeMove`/`MakeNullMove`/`SetSquare`). Нужен только для O(1) точечного чтения (`PieceAt`) и для кода, которому нужна вся доска разом (`SquaresArray()` — копия массива; используется, например, `SEE`, и раньше использовался `eval`, пока тот тоже не перешёл на битборды).

**`SetSquare(sq, p)` — единственный поддерживаемый способ вручную
поправить позицию** (тестовые хелперы, конструирование кастомных
досок) в обход `MakeMove`. Прямая запись в `squares` напрямую (поле
приватное, так и было изначально задумано, но раньше называлось
`Squares` и было экспортировано — при миграции переименовано в
`squares` именно чтобы закрыть эту дыру: любой прямой доступ снаружи
пакета `board` теперь не компилируется вообще, а не просто "не
рекомендуется") молча рассинхронизировала бы `bb`, оставляя `squares`
и битборды в противоречии — обнаружилось на практике как минимум один
раз (тестовый хелпер, расставлявший фигуры до перехода на
`SetSquare`), вызывало падение генерации ходов с паникой при индексации
по несуществующей клетке.

**`Board.KingSquare(c)` кэширует позицию короля**, а не сканирует все
64 клетки на каждый вызов. Кэш (`Board.kingSq`) поддерживается в
`MakeMove`/`UnmakeMove` при ходе королём (включая обе рокировки).
Кэш не считается источником истины: перед использованием `KingSquare`
сверяет, что клетка из кэша ДЕЙСТВИТЕЛЬНО всё ещё держит короля
нужного цвета, и если нет — откатывается на честный full-scan и сам
чинит кэш для следующих вызовов.

### Magic bitboards (`magic.go`)

Слайдеры (слон/ладья/ферзь) получают атаки за O(1): маска релевантной
occupancy на клетке, умножение на magic-число, сдвиг — индекс в
предпосчитанную таблицу атак для этой клетки и этого паттерна
занятости. Сами magic-числа найдены офлайн случайным поиском (не
частью движка) и независимо перепроверены вторым скриптом, перебравшим
все occupancy-подмножества для всех 64 клеток и подтвердившим ноль
коллизий — корректность самого движка при этом не зависит от доверия к
поиску: маски, "честные" ray-walk атаки для заполнения таблиц и
арифметика индексации целиком в Go, при любом плохом числе тесты
(`board`-пакета + `TestMagicSlidingMatchesRayWalk`, уже удалён после
подтверждения) поймали бы расхождение.

### Инкрементальные поля: `hash`, `bb`, `materialPST`

Все три поддерживаются одинаково — рядом с каждой мутацией доски в
`MakeMove`/`UnmakeMove`, а не пересчитываются с нуля на каждый вызов:

- **`hash`** — Zobrist, XOR точечно на изменившееся. `UnmakeMove`
  восстанавливает `HashBefore` снэпшотом (не обратным XOR).
- **`bb`** — `put`/`remove`/`move` на изменившиеся фигуры, зеркально в
  обе стороны (`MakeMove` и `UnmakeMove` оба явно вызывают эти методы,
  не снэпшот).
- **`materialPST`** — сумма `PieceValue(p, sq)` (хук из
  `material_hook.go`, реальные веса регистрирует `eval`) по всем
  фигурам КРОМЕ короля — `UnmakeMove` восстанавливает снэпшотом
  (`MaterialPSTBefore`), как хеш, а не обратным пересчётом. Король
  исключён намеренно: его позиционная ценность зависит от `phase`
  (глобальная величина, меняется при любом взятии/превращении фигуры
  где угодно на доске, не только при ходе самого короля) — `eval`
  считает king-term отдельно, на лету, при каждом вызове `Evaluate`
  (это дёшево — два табличных чтения от уже закэшированного
  `KingSquare`, не скан).

`NewInitialBoard`/`FromFEN` — единственные два места, которые сеют все
три поля с нуля (`computeHashFromScratch`/`rebuildBitboards`/
`rebuildMaterialPST`). `VerifyHash`/`VerifyBitboards`/
`VerifyMaterialPST` сверяют инкремент с честным пересчётом — гоняются
по perft-дереву в тестах, а не доверяются на слово.

**Move application — make/unmake, не копирование.** Стандартная
дисциплина вызова — как в стеке рекурсии:
```go
undo := b.MakeMove(m)
// ... смотрим на позицию, рекурсируем ...
b.UnmakeMove(m, undo)
```

**`Board.Copy()` остаётся в API** для мест, где ветка одноразовая и
её нечем/незачем откатывать, а исходную (часто ЧУЖУЮ, живую) позицию
трогать нельзя: `search.principalVariation`, `movegen.checkSuffix` в
`notation.go`. Любой код, который держит указатель на позицию "до
хода" и ожидает, что `MakeMove` где-то ещё её не тронет, обязан явно
взять `Copy()` заранее — само по себе это не гарантируется.

## `internal/movegen`

`GenerateLegalMoves` генерирует ходы **напрямую как легальные**, не
generate-then-filter:

1. `Checkers(b)` — битборд фигур, держащих короля стороны-к-ходу под
   шахом (0, 1 или 2 — двойной шах разрешает только ход королём).
2. `Pins(b)` — для каждой связанной фигуры маска клеток, куда ей всё
   ещё можно ходить (линия до короля + сама связывающая фигура).
3. Ход короля — отдельная проверка на каждую клетку-кандидата
   (`squareAttackedWithOccupancy` с occupancy БЕЗ самого короля —
   иначе король "не видел бы", что не может отступить назад по линии
   шаха, которую сам же и блокировал).
4. Остальные фигуры — их обычный attack-битборд пересекается с
   `checkMask` (при шахе: взять шаховщика или заблокировать линию) и
   `pinMask` (если связана).

Единственное оставшееся исключение с реальным `MakeMove`/
`IsSquareAttacked`/`UnmakeMove` — взятие на проходе: снимает две пешки
с одной горизонтали разом, что может вскрыть шах по этой горизонтали
даже если ни одна из пешек по отдельности не была связана. Редкий
случай (максимум один кандидат на позицию), так что цена честной
проверки пренебрежима — то же решение принимает практически любой
битбордовый движок для этого конкретного случая.

`GenerateLegalCaptures` — отдельная генерация **только взятий**,
напрямую из битбордов (без прохода по тихим ходам вообще), для
`quiescenceSearch`, где подавляющее большинство узлов не под шахом и
интересуются только взятиями. **Не заменяет** `GenerateLegalMoves` для
проверки мата/пата — пустой результат означает только "нет взятий", не
"нет ходов вообще".

`generateLegalMovesSlow` — старый generate-then-filter генератор,
временно оставлен как доверенный эталон (`VerifyLegalMoveGeneration`,
`movegen_verify_test.go`, гоняется по perft-дереву стартовой позиции и
Kiwipete на каждую правку). Кандидат на удаление отдельным коммитом,
пока не убран.

`IsSquareAttacked`/`AttackersTo` — табличные/magic lookup'ы по всем
типам фигур; `AttackersTo` — то же самое, но возвращает битборд всех
атакующих, а не bool (нужен `Checkers`/`Pins`).

`GameStatus`/`GameStatusWithHistory`/`InsufficientMaterial` — без
изменений в этой миграции (см. прошлую версию документа за деталями,
не менялись).

## `internal/eval`

Материал + piece-square tables (`pst.go`, тейпинг midgame/endgame по
фазе — только для короля) + mobility + пешечная структура + king
safety + пара слонов + mop-up. Все термы читают битборды через
`Board.Pieces`/`Occupied`/`OccupiedBy` — ни один больше не сканирует
64 клетки вручную.

**Материал+PST — почти целиком инкрементальный.**
`materialAndPstScore` — это `b.MaterialPST()` (одно чтение поля,
поддерживается в `board`) плюс king-term, посчитанный на лету по
текущей `phase`.

**`PawnCache` (`pawn_cache.go`)** — per-поиск кэш `pawnStructureScore`,
ключ — микс двух битбордов пешек (не позиция целиком: пешечная
структура одна и та же для огромного числа узлов дерева, где ходят не
пешки). Живёт внутри `search.TranspositionTable` (один кэш на один
вызов `BestMove`/`BestMoveTimed`), **не** как глобальная переменная
пакета — глобальный кэш потребовал бы мьютекса в момент, когда поиск
станет многопоточным, а так каждый поиск просто получает свой.
`Evaluate(b, cache)` принимает `cache` как `nil`-safe параметр — `nil`
означает "без кэша", не ошибка.

## `internal/perft`

`Perft(b, depth)`/`Divide(b, depth)` — брутфорс-подсчёт листьев дерева
легальных ходов. `VerifyHashes`/`VerifyBitboards` идут по тому же
дереву и на каждом узле сверяют `Board.VerifyHash()`/`VerifyBitboards()`
с честным пересчётом. Эталонные числа — стартовая позиция и Kiwipete.

(`VerifyMaterialPST`/`VerifyLegalMoveGeneration` — тот же приём, но
живут не здесь, а в `internal/eval`/`internal/movegen` соответственно,
там, где регистрируются нужные им хуки/эталонные генераторы — см. эти
разделы.)

## `internal/search`

**`TranspositionTable` — фиксированный массив, не `map`.**
`entries []ttEntry` размером `ttSize = 1<<20` (степень двойки —
индексация `hash & ttMask`, не modulo). Слотов меньше, чем возможных
хешей, так что две разные позиции могут претендовать на один слот —
`probe`/`store` хранят полный хеш в самой записи и сверяют его,
несовпадение — честный промах кэша, не ошибка. Замещение —
depth-preferred: чужая (другой хеш) запись с бо́льшей глубиной
сохраняется вместо более мелкой новой; свежий результат той же самой
позиции или пустой слот — всегда перезаписывается. В отличие от `map`,
у массива фиксированный объём памяти — не растёт неограниченно за
время долгого поиска.

**`history` — тоже массив, не `map`.** `[64*64]int`, индекс
`From*64+To` — пар всего 4096, хэш-мапа была не нужна с самого начала.

**Move ordering:** TT-ход → взятия по MVV-LVA → killer moves → history
heuristic. Финальная сортировка кандидатов — не `sort.Sort`/
`sort.Slice`, а прямая вставочная сортировка
(`insertionSortByScoreDesc`): списки ходов в этом движке маленькие
(редко больше нескольких десятков), `pdqsort`'у там нечего
оптимизировать, а `sort.Sort`'а интерфейсная диспетчеризация
(`Len`/`Less`/`Swap`) — чистый оверхед на такой длине.

**Null-move pruning** — с защитой от цугцванга (`hasNonPawnMaterial`)
и от срабатывания в корне (`isRoot`).

**Расширение на шах.** `negamax` продлевает глубину на 1 ply для хода,
который ставит сопернику шах (не декрементирует `depth` для дочернего
вызова), пока не исчерпан бюджет `checkExtLeft`
(`maxSearchCheckExtensions = 6` на всю линию, тот же принцип, что уже
был у `quiescenceSearch`'а `maxQuiescenceCheckExtensions`) —
предотвращает ситуацию, когда горизонт поиска обрывается ровно
посреди форсированной серии шахов. Шахующие ходы также исключены из
LMR (форсированные, не "вероятно неважные тихие ходы").

**Futility pruning.** На малой оставшейся глубине (`depth <=
futilityMaxDepth = 3`), не в корне, не под шахом: тихий ход (не
взятие, не превращение, сам не ставит шах), для которого даже
`staticEval + futilityMargin[depth]` не достаёт до `alpha`, отбрасывается
без единого рекурсивного вызова — статическая оценка с запасом
показывает, что у него физически нет шанса поднять счёт настолько.
Не применяется к первому ходу в списке (`i == 0` — тот, на который уже
поставило move ordering, та же осторожность, что у PVS/LMR).

**LMR встроен внутрь PVS**, не отдельной веткой. Первый ход — сразу
полное окно `(-beta, -alpha)`. Каждый следующий — сначала дешёвая
проба нулевым окном; для поздних тихих ходов (после
`lmrFullDepthMoves`-го, на глубине `>= lmrMinDepth`, не взятие/
превращение/шах, не под шахом) первая проба идёт на уменьшенной
глубине, дальше — подтверждающая проба / полный пересчёт по обычной
PVS-логике.

**Aspiration windows** — без изменений в этой миграции.

**SEE (`see.go`) — на битбордах, не копия `[64]Piece`.** `seeBoard` —
собственный лёгкий мутируемый снимок (`[2][7]Bitboard` — цвет×тип,
плюс occupancy), сидится один раз из реального `Board` через
`Pieces`/`Occupied`, дальше мутируется локально по ходу симуляции
размена (`put`/`remove`) — ни одной копии `[64]Piece` не строится.
`leastValuableAttacker` — те же табличные/magic lookup'ы, что и
`AttackersTo`, по возрастанию ценности с ранним выходом.

При **нескольких атакующих одного типа и цвета** (несколько ферзей/
слонов/ладей/коней/пешек одновременно бьют по клетке — редко в
реальной игре, но не невозможно) выбор конкретного атакующего не
косметический: разные фигуры могут вскрывать разные рентген-атаки при
снятии, меняя весь дальнейший размен. `pickAttacker`/
`pickPawnAttacker`/`pickKnightAttacker` разрешают такую ничью, сверяя
направление кандидата от целевой клетки с фиксированным порядком
проверки (тем же, что был у старой ray-walk-реализации) — не просто
берут "первую попавшуюся" по номеру клетки.

Используется в `quiescenceSearch` (отсечение взятий с `SEE < 0`,
сортировка оставшихся по настоящему SEE); в ordering'е `negamax` по
всему дереву НЕ используется — там MVV-LVA.

**`quiescenceSearch`.** Под шахом — полный `GenerateLegalMoves`
(нужен для мата/пата и полного списка уклонений). Не под шахом
(подавляющее большинство узлов) — `GenerateLegalCaptures`, без
генерации тихих ходов вообще. Не отличает "взятий нет, но тихие ходы
есть" от "на самом деле пат" в этой ветке — сознательный компромисс
(редкий случай, `negamax`'а собственная терминальная проверка остаётся
полностью корректной).

**`SearchInfo`/`...Info`-варианты** — без изменений в этой миграции.

**Книга дебютов** — без изменений в этой миграции.

## `internal/book`, `internal/uci`, `cmd/cli`, `cmd/wasm`, `web/`

Без изменений в этой миграции — см. предыдущую версию документа или
сами файлы, публичный API этих пакетов эта работа не трогала.

## `cmd/profile`

`go run ./cmd/profile [-depth=N] [-out=cpu.prof]` — гоняет
`search.BestMove(board.NewInitialBoard(), depth)` (по умолчанию
`depth=6`) под CPU-профилем Go, пишет `cpu.prof` в текущую директорию.
Дальше — `go tool pprof -top cpu.prof`. Постоянный инструмент проекта,
не одноразовый скрипт — использовался на каждом шаге этой миграции для
проверки, что очередная оптимизация действительно что-то ускорила, а
не просто выглядит правильной на бумаге.