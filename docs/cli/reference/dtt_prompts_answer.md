## dtt prompts answer

Answer or cancel a pending SSH question

### Synopsis

Answer an SSH question, like typing into ssh's password prompt. Read the answer from a hidden TTY prompt or --stdin, never from a flag or command argument. Without a TTY, --stdin is required (exit 4 otherwise). --cancel cancels without reading input. Ctrl-C while answering cancels the question.

```
dtt prompts answer <id> [flags]
```

### Examples

```
  dtt prompts answer PROMPT_ID
  printf 'yes\n' | dtt prompts answer PROMPT_ID --stdin
  dtt prompts answer PROMPT_ID --cancel
```

### Options

```
      --cancel   Cancel the question without reading an answer
  -h, --help     help for answer
      --stdin    Read the answer's first line from standard input
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

* [dtt prompts](dtt_prompts.md)	 - List SSH questions waiting for an answer

