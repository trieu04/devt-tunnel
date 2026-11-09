## devt-tunnel prompts

List SSH questions waiting for an answer

### Synopsis

List pending SSH questions, such as the host-key and password questions normally displayed by ssh. Answers are never shown. Use devt-tunnel prompts answer to respond or cancel a question.

```
devt-tunnel prompts [flags]
```

### Examples

```
  devt-tunnel prompts
  devt-tunnel prompts --json
  printf 'yes\n' | devt-tunnel prompts answer PROMPT_ID --stdin
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

* [devt-tunnel](devt-tunnel.md)	 - Supervise and inspect development tunnels
* [devt-tunnel prompts answer](devt-tunnel_prompts_answer.md)	 - Answer or cancel a pending SSH question

