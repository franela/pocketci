# webhook

`webhook` is a Dagger module that leverages the Dagger SDK to wrap [webhook](https://github/adnanh/webhook) with some nice functionalities. It allows you to run dagger commands (and any other kind of commands) in one of two ways:
- Portable CI runner: By starting the service as is you can hook it to a repository through webhooks and run commands in the context of your repo (like doing a `checkout` on github actions first!)
- Webhook server: By providing a `hooks.json` on startup you can leverage Dagger modules to hook them into specific webhooks (like a Slack Webhook)

## Portable CI runner
1. Start the service: `dagger up -m github.com/franela/webhook`
2. Expose it to the internet (you can use the likes of `ngrok` to expose local endpoints)
3. Connect your repository by going to the VCS configuration
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

The working directory of your command will contain the entire contents of your repository at the specified REF so you could run commands from your own dagger module.

## Webhook server
1. Start the service: `dagger up -m github.com/franela/webhook --hooks hooks.json`
