# Summarize text with OpenAI

This command reads text from stdin, calls the released OpenAI adapter and prints
its validated `Summary.Text`. It registers no tools. Output depends on the model.

## Run

Requires Go 1.25.5+, provider access to `gpt-5.4`, network access and a billable
OpenAI API credential. Load `SUMMARY_OPENAI_API_KEY` privately through your secret
manager or environment; never paste it into tracked configuration or a command.

From this checkout:

```sh
printf '%s\n' 'Shipment 42 is delayed two days. Notify the customer.' | \
  go run ./examples/summary
```

The local process acts for its operator. In a web application, call the same
`summarize` operation with an actor from verified server context, never request
JSON. `OnlyTools()` denies every tool; no credentials or identity become model
arguments. Context and token limits bound work, not provider billing.

## Test without a provider call

```sh
go test ./examples/summary
```

The test uses the real OpenAI adapter against a controlled HTTP server. It checks
the structured-output request and typed result. It does not validate live model
availability, inference quality or billing. No real credential is needed.

The [purchase-review fixture](../purchase-review/README.md) separately exercises
tool authorization and denied paths with the scripted provider.
