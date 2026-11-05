## dtt prompts

List SSH questions waiting for an answer

### Synopsis

List pending SSH questions, such as the host-key and password questions normally displayed by ssh. Answers are never shown. Use dtt prompts answer to respond or cancel a question.

```
dtt prompts [flags]
```

### Examples

```
  dtt prompts
  dtt prompts --json
  printf 'yes\n' | dtt prompts answer PROMPT_ID --stdin
```

### Options

```
  -h, --help   help for prompts
```

### Options inherited from parent commands

```
      --json               Write versioned JSON results and errors
      --no-color           Disable color (also NO_COLOR)
      --no-daemon-start    Do not start the daemon automatically
      --no-input           Never prompt for input
  -q, --quiet              Suppress informational diagnostics
      --timeout duration   Daemon connection and request timeout (default 10s)
```

### SEE ALSO

* [dtt](dtt.md)	 - Supervise and inspect development tunnels
* [dtt prompts answer](dtt_prompts_answer.md)	 - Answer or cancel a pending SSH question

