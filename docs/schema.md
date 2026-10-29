# Supported schemas

`SchemaFor[T]`, `NewTool`, and `New` build schemas once and return construction
errors for unsupported boundaries. Tool arguments, tool results, and final
output must have struct roots. Empty argument structs are supported.

Supported fields are named structs, strings, booleans, signed/unsigned integers,
float32/float64, and slices of supported values. Named scalar types work unless
they define custom JSON or text marshaling. Every exported struct field needs
an explicit `json:"name"` tag. All fields are required and non-null; an empty
slice is `[]`, not `null`. Integers reject decimal/exponent lexical forms and
are range-checked against the actual Go type. Floats must fit the Go type.

Optional `enum:"a,b"` tags restrict string fields. Empty/duplicate enum entries
and non-string enums fail construction. `description:"..."` supplies metadata.
Generated objects use `additionalProperties:false` and explicit `required`
lists. Go numeric ranges are enforced locally, not encoded into JSON Schema.

Unsupported types include pointers/nullable values, maps, interfaces, fixed
arrays, byte slices (including aliases), embedded/unexported fields, recursive
schemas, custom marshal/unmarshal methods, `time.Time`, `omitempty`, `string`
tag options, ignored fields, and duplicate JSON names. Schema nesting is capped
at 16 and generated schema JSON at 64 KiB. This intentionally does not implement
general JSON Schema or Pydantic's type system.

Decoding uses a bounded, depth-limited token parse, rejects duplicate object
keys and trailing data, validates exact names/required fields/types/enums, then
decodes into the exact Go boundary. JSON nesting is capped at 32 and raw UTF-8
must be valid. Standard Go JSON string escape semantics apply. It does not rely
on plain `json.Unmarshal` as validation.

Caller validators add business constraints: argument `Validate`, tool
`ValidateResult`, and agent `ValidateOutput` receive context and run dependencies.
Their errors are not sent verbatim to the model; repair feedback is generic to
avoid leaking host errors. Validators should be deterministic, read-only, and
cooperative with cancellation. Tools use a canonical snapshot after argument
validation. Resource policy, approval verification, and handler execution get
separate argument copies. Tool-result and final-output data are schema checked
before acceptance. A final event gets a separate copy from the returned result.

Pre-execution argument validation can consume a repair attempt. Tool-result
validation failure stops the run: the handler already ran and is not retried.
Malformed final output and business output validation failures can consume a
repair attempt. A repaired final response cannot retroactively undo an effect.
