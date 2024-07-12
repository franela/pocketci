# pocketci

**This is very much a work in progress, expect things to be rough and potentially break!**

`pocketci` is the first small and trully portable CI engine. This project leverages the Dagger SDK to wrap [webhook](https://github.com/adnanh/webhook) with ability to automatically clone repositories. It allows you to run dagger commands (and any other kind of commands) in one of two ways:

- Portable CI runner: By starting the service as is you can hook it to a repository through webhooks and run commands in the context of your repo (like doing a `checkout` on github actions first!)
- Webhook server: By providing a `hooks.yaml` on startup you can leverage Dagger modules to hook them into specific webhooks (like a Slack Webhook)

### Next steps

Using [webhook](https://github.com/adnanh/webhook) allowed us to quickly test the idea of a CI engine that by leveraging Dagger in a native way becomes truly portable. The use of this project means we are limited by the definitions of the `hooks.yaml` format which, while being very useful, does not integrate in a native way with Dagger. We want pocketci to integrate seamlessly with Dagger and Dagger modules. You can join the discussion on how to build that [here](https://github.com/franela/pocketci/issues/2).

## Portable CI runner

1. Start the service: `dagger call -m github.com/franela/pocketci/module@pocketci-agent serve --src https://github.com/franela/pocketci#pocketci-agent up`
2. Expose it to the internet (you can use the likes of `ngrok` to expose local endpoints)
3. Connect your repository by setting up a webhook in your VCS (github is the only supported at the moment)
4. Write a hooks.yaml file that triggers a dagger command on every commit to the main branch. For example on a github repo:

```yaml
- id: git-push
  execute-command: "/bin/dagger"
  include-command-output-in-response: true
  command-working-directory: "/hooks-test"
  pass-arguments-to-command:
  - source: string
    name: call
  - source: string
    name: container-echo
  - source: string
    name: "--string-arg"
  - source: payload
    name: commits.0.message
  trigger-rule:
    and:
    - match:
        type: payload-hmac-sha1
        secret: itsasecret
        parameter:
          source: header
          name: X-Hub-Signature
    - match:
        type: value
        value: refs/heads/main
        parameter:
          source: payload
          name: ref
```

The working directory of your command will contain the entire contents of your repository at the specified `ref` so you could run commands from your own dagger module.

By default it proxies requests in a synchronous way, meaning that the execution of your commands will block the incoming requests. This could be a problem if you are exposing this to a provider such as a Github that has a [timeout of 10 seconds](https://docs.github.com/en/webhooks/testing-and-troubleshooting-webhooks/troubleshooting-webhooks#timed-out). To force every request to be processed in an async way, you can pass the flag `--proxy-async`.

## Webhook server

Write a `hooks.yaml`. For example one that runs a dagger call on every single request:

```yaml
- id: webhook
  execute-command: dagger
  include-command-output-in-response: true
  response-headers:
    - name: "Access-Control-Allow-Origin"
      value: "*"
  pass-arguments-to-command:
    - source: "string"
      value: "--debug"
    - source: "string"
      value: "--progress=plain"
    - source: "string"
      value: "call"
    - source: "string"
      value: "-m"
    - source: "string"
      value: "github.com/shykes/daggerverse/hello"
    - source: "string"
      value: "Hello World!"
```

Run it with:

```console
dagger call \
 -m github.com/franela/pocketci/module@pocketci-agent \
 serve \
  --src https://github.com/franela/pocketci#pocketci-agent \
  --hooks hooks.yaml \
 up
  ```

  You can validate that your webhook is triggered: `curl http://localhost:8080/hooks/webhook`. You should see a response such as:

```
1: load call
1: loading module
1: loading module [6.57s]
1: loading objects
1: loading objects [3.02s]
1: traversing arguments
1: traversing arguments [0.00s]
1: load call DONE

2: dagger call message
DEBUG: executing query="query{hello{message}}"
Hello, World!
2: dagger call message DONE
```
