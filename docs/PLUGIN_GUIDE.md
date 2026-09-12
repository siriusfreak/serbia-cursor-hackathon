# Как добавить плагин

Ядро трогать не нужно. Плагин — это Go-пакет в `exts/`, реализующий три метода.

## За пять минут

**1. Скопируй эталон**

```bash
cp -r exts/profile exts/mytool && rm exts/mytool/*_test.go
```

**2. Опиши себя в `Manifest()`**

```go
func (e *Ext) Manifest() ext.Manifest {
	return ext.Manifest{
		Name:       "mytool",            // snake_case, глобально уникально
		Version:    "0.1.0",
		ABIVersion: ext.ABIVersion,      // всегда так, не хардкодь число
		Kind:       ext.KindRetrieval,
		Provides: []ext.ToolSpec{{
			Name:        "search",
			Description: "Ищет в документации. Вызывай, когда нужен факт из внешнего источника.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}},"required":["q"]}`),
			ReadOnly:    true,
		}},
	}
}
```

Тул будет виден модели как **`mytool_search`** — `<плагин>_<тул>`.

**3. Реализуй `Invoke()` — switch по именам**

```go
func (e *Ext) Invoke(ctx context.Context, tool string, in json.RawMessage) (json.RawMessage, error) {
	switch tool {
	case "search":
		var a struct{ Q string `json:"q"` }
		if err := ext.Args(in, &a); err != nil {
			return nil, err
		}
		if a.Q == "" {
			return nil, ext.Invalidf("q пустой; передай строку запроса")
		}
		return ext.JSON(map[string]any{"hits": hits})
	default:
		return nil, ext.Invalidf("mytool не имеет тула %q", tool)
	}
}
```

**4. `Close()`** — освободи своё. Чужое (например переданный `*store.Store`) не закрывай.

**5. Зарегистрируй в `cmd/cogdebt/main.go`**

```go
reg.MustLoad(
	profile.New(db, userID),
	analogy.New(),
	mytool.New(),        // ← одна строка
)
```

Всё. Модель увидит новый тул на следующем же ходу.

## Тест — обязателен, он же бесплатный

```go
func TestConformance(t *testing.T) {
	exttest.Conformance(t, mytool.New())
}
```

`Conformance` прогоняет ровно то, что хост проверит при загрузке, плюс сценарии, которые всплывают только на сцене: битые аргументы, неизвестное имя тула, двойной `Close`. Реальные вызовы — через `exttest.Calls`, пример в [`exts/profile/profile_test.go`](../exts/profile/profile_test.go).

```bash
go test ./exts/...
```

## Ошибки: `Fault`, а не `error`

Ошибка обязана пережить границу процесса, поэтому она — данные:

```go
return nil, ext.Invalidf("skills пустой; передай хотя бы один навык")
return nil, ext.NotFoundf("профиль пуст, сначала вызови profile_upsert")
return nil, ext.Internalf("чтение базы: %v", err)
```

**`Message` уходит обратно в LLM как результат тула.** Пиши его как инструкцию модели — «сначала вызови profile_upsert», а не «nil pointer dereference». Модель прочитает и исправится сама на следующем ходу. Обычный `error` (не `Fault`) тоже не роняет прогон, но громко пишется в лог как поломка.

## Четыре правила, которые нельзя нарушать

**1. Через границу — только байты.** Ни указателей, ни каналов, ни `fyne.CanvasObject`. Нарушишь — плагин навсегда останется in-process, и вынести его в отдельный процесс будет нельзя.

**2. `Manifest()` чистый, дешёвый, стабильный.** Хост зовёт его на каждом ходу, чтобы пересобрать список тулов. Никаких походов в сеть и в базу.

**3. Плагин не зовёт другой плагин.** Нужна чужая способность — объяви `Requires: []ext.Kind{...}`, вызов сделает хост. Это то правило, которое ломают первым, и после него «плагинность» остаётся только на бумаге.

**4. Нет состояния между вызовами, которое нельзя восстановить.** Субпроцесс могут перезапустить в любой момент. Состояние живёт в `store`.

## Где хранить данные

Не добавляй таблицы — возьми именованное KV:

```go
kv := db.KV("mytool")           // пространство имён только твоё
kv.SetJSON(ctx, "cache", data)
ok, err := kv.GetJSON(ctx, "cache", &data)
```

Нужны свои таблицы — `db.DB()` отдаёт `*sql.DB`.

**Важно:** плагин, принимающий `*store.Store`, становится **in-process-only**. Это нормально для плагинов, которые и есть хранилище хоста (как `profile`). Плагину, который должен уметь жить в отдельном процессе, храни состояние у себя.

## Виды плагинов

| Kind | Что делает |
|---|---|
| `profile` | профиль и mastery |
| `retrieval` | поиск, GitHub, доки |
| `assessor` | вопросы и оценка |
| `llm` | описание эндпоинта модели |
| `ui` | ViewSpec-JSON для рендера в Fyne |
| `agent` | описание суб-агента |

## Агент как плагин

`llm` и `agent` — **декларативные**: они не реализуют логику, а описывают её. Ровно один тул с именем `ext.DescribeTool`, возвращающий спеку; хост строит агента сам.

```go
func New() *Ext {
	return &Ext{spec: ext.AgentSpec{
		Description: "Строит аналогию. Делегируй сюда, когда профиль заполнен.",
		Instruction: "...системный промпт...",
		ToolRefs:    []string{"profile_get"},
		SkipSummarization: true,
	}}
}
```

**Новый агент = новый манифест, без единой строки логики.** Пример на двух стратегиях — [`exts/analogy/analogy.go`](../exts/analogy/analogy.go).

Две ловушки:

- **`SkipSummarization: false`** (значение по умолчанию) добавляет **лишний вызов LLM** на каждое делегирование — цена и задержка удваиваются незаметно. Для детерминированных агентов ставь `true`.
- **Циклы.** Агент А ссылается в `ToolRefs` на агента Б, тот на А. Хост ловит это и режет по глубине (`MaxAgentDepth`, по умолчанию 2), но лучше не строить.

## UI-плагин

UI-плагин **не возвращает виджеты** — он возвращает декларативный ViewSpec-JSON, а рисует его хост. Иначе плагин пришлось бы линковать с Fyne, и он навсегда остался бы in-process.

```go
return ext.JSON(map[string]any{"view": ext.Stack(
    ext.Markdown("Ты знаешь K8s. Пайплайн — тот же reconcile loop…"),
    ext.View(ext.ViewAnalogyTable, "", ext.AnalogyTableProps{Rows: []ext.AnalogyRow{{
        Source: "etcd", Target: "feature store", SharedRole: "source_of_truth",
        CarryOver: "Двойная запись — это split-brain.",
        Breakdown: "Point-in-time корректности у etcd никогда не было.",
    }}}),
    ext.View(ext.ViewQuestion, "q1", ext.QuestionProps{Level: "L2", Prompt: "Где аналогия ломается?"}),
)})
```

Словарь закрытый и маленький: `stack · markdown · analogy_table · question · mastery`. Неизвестный `type` рисуется плашкой-заглушкой, окно не падает — плагин новее хоста деградирует, а не ломается.

Тип целиком в [`internal/ext/viewspec.go`](../internal/ext/viewspec.go), рендер — в [`internal/ui/render.go`](../internal/ui/render.go).

**Посмотреть, как это выглядит,** без единого вызова модели:

```bash
go run ./cmd/cogdebt -screenshot /tmp/preview.png
```

## Схемы: одна ловушка

`Schema` обязана быть `{"type":"object", ...}` — иначе плагин отклонят при загрузке. Внутри — обычный JSON Schema в нижнем регистре (`"string"`, `"array"`). Конвертацию в формат genai (`"STRING"`, `"ARRAY"`) хост делает сам, рекурсивно. Руками регистр не меняй.

## Что будет, если плагин кривой

Он **не загрузится**, в лог уйдёт причина, остальные продолжат работать. Хост не падает никогда — иначе один плохой плагин убивает демо.

Проверить, что видит хост:

```bash
go run ./cmd/cogdebt -debug
```

Баннер печатает каждый загруженный плагин и число тулов, отданных модели.

## Весь контракт

Один файл: [`internal/ext/abi.go`](../internal/ext/abi.go). Больше ничего читать не нужно.
