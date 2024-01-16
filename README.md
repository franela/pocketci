# webhook

**This is very much a work in progress, expect things to be rough and potentially break!**

`webhook` is a Dagger module that leverages the Dagger SDK to wrap [webhook](https://github/adnanh/webhook) with some nice functionalities. It allows you to run dagger commands (and any other kind of commands) in one of two ways:
- Portable CI runner: By starting the service as is you can hook it to a repository through webhooks and run commands in the context of your repo (like doing a `checkout` on github actions first!)
- Webhook server: By providing a `hooks.json` on startup you can leverage Dagger modules to hook them into specific webhooks (like a Slack Webhook)

## Portable CI runner
1. Start the service: `dagger up -m github.com/franela/webhook -p 8080:8080`
2. Expose it to the internet (you can use the likes of `ngrok` to expose local endpoints)
3. Connect your repository by setting up a webhook in your VCS (github is the only supported at the moment)
4. Write a hooks.json file that triggers a dagger command on every commit to the main branch. For example on a github repo:
```json
{
  "id": "webhook",
  "execute-command": "dagger",
  "include-command-output-in-response": true,
  "response-headers": [
    {
      "name": "Access-Control-Allow-Origin",
      "value": "*"
    }
  ],
  "pass-arguments-to-command": [
    {
      "source": "string",
      "name": "--debug"
    },
    {
      "source": "string",
      "name": "--progress=plain"
    },
    {
      "source": "string",
      "name": "call"
    },

    {
      "source": "string",
      "name": "-m"
    },
    {
      "source": "string",
      "name": "github.com/shykes/daggerverse/hello"
    },
    {
      "source": "string",
      "name": "message"
    }
  ],
  "trigger-rule": {
    "and": [
      {
        "match": {
          "type": "value",
          "value": "refs/heads/main",
          "parameter": {
            "source": "payload",
            "name": "ref"
          }
        }
      }
    ]
  }
}
```

The working directory of your command will contain the entire contents of your repository at the specified `ref` so you could run commands from your own dagger module.

## Webhook server
Write a `hooks.json`. For example one that runs a dagger call on every single request:
```json
{
  "id": "webhook",
  "execute-command": "dagger",
  "include-command-output-in-response": true,
  "response-headers": [
    {
      "name": "Access-Control-Allow-Origin",
      "value": "*"
    }
  ],
  "pass-arguments-to-command": [
    {
      "source": "string",
      "name": "--debug"
    },
    {
      "source": "string",
      "name": "--progress=plain"
    },
    {
      "source": "string",
      "name": "call"
    },

    {
      "source": "string",
      "name": "-m"
    },
    {
      "source": "string",
      "name": "github.com/shykes/daggerverse/hello"
    },
    {
      "source": "string",
      "name": "message"
    }
  ]
}
```

Run it with: `dagger up -p 8081:8080 webhook --hooks hooks.json`. You can validate that your webhook is triggered: `curl http://localhost:8080/hooks/webhook`. You should see a response such as:
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

**NOTE:** there is currently an [issue]() with the use of services in the context of dagger modules. If you re-run this process again Dagger will be stuck at `starting session`. The quick-fix at the time of this writing is to `docker rm -f` the Dagger engine and start over.
