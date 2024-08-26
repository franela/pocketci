# pocketci

**This is very much a work in progress, expect things to be rough and potentially break!**


`pocketci` is a trully portable CI engine. It builds on top of [dagger](https://dagger.io), adding functionalities commonly needed when building CI pipelines for your projects. 

> [!NOTE]
> At the moment `GitHub` is the only supported VCS

In a nutshell, `pocketci` moves the dispatching logic from your workflow YAMLs to your Dagger modules. You wire it to your VCS of choice via [Webhooks](https://docs.github.com/en/webhooks/about-webhooks) and it takes care of calling the module in charge of orchestrating your CI.


## Getting started

> [!CAUTION]
> This project is still under active development and brainstorming, things can drastically change and break

`pocketci` is a golang application that wraps the dagger engine with some functionalities necessary for running your CI infrastructure. Running it locally requires three environment variables:
```terminal
# set the git credentials used by pocketci to clone the repositories
export GITHUB_USERNAME=<YOUR GITHUB USERNAME>
export GITHUB_TOKEN=<YOUR PAT OR FINE GRAINED TOKEN>
export X_HUB_SIGNATURE=<SECRET SIGNATURE CONFIGURED FOR WEBHOOKS>
```

With that configured you can then simply:
```sh
go run ./cmd/agent
```

### Guide

At the moment `pocketci` is building `pocketci`. This means we can look at how this happens to understand how to use it. This is a traditional Go application that needs to be built, tested & released. Our workflow requirements are:
- On every `commit` pushed to `main` publish an image with the commit sha to ghcr
- On every `pull_request` run the tests

With `GitHub` actions we would build one workflow for each of the triggers and make the corresponding `dagger call`:
```yaml
-- .github/workflows/release.yml
name: Publish Release

on:
  push:
    tags:
      - 'v[0-9]+.[0-9]+.[0-9]+'

jobs:
  build-and-push-release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - name: Build and push
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: |
          dagger call -m ./ci publish --src . --tag {{ github.ref }}

-- .github/workflows/publish-main.yml
name: Publish @ main

on:
  push:
    branches:
      - main

jobs:
  build-and-push-main:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Build and publish
        run: |
          dagger call -m ./ci publish --src . --tag {{ github.sha }}

-- .github/workflows/check.yml
name: Tests

on:
  pull_request:
    branches:
      - main

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Run tests

run: |
          dagger call -m ./ci test --src .
```

With `pocketci` you instead take care of interpreting the `triggers` defined above within your module. First you define the location of your dispatcher module, what secret parameters it needs and which `events` trigger a call to it:
```yaml
module-path: ./ci
events:
  pull_request: 
  - main
  tags: 
  - 'v[0-9]+.[0-9]+.[0-9]+'
  commits:
  - main
paths:
  - "**/**.go"
secrets:
  - name: ghUsername
    from-env: GITHUB_USERNAME
  - name: ghPassword
    from-env: GITHUB_TOKEN
```

Then, in your `./ci` dagger module you implement a `Dispatch` function that accepts the source directory, eventTrigger and your configured secrets. In that function you can parse the event that triggered the call using `pocketci`'s helper module:
```go
// `ghUsername` and `ghPassword` are automatically mapped by pocketci using what you specify in the `pocketci.yaml`
func (m *Ci) Dispatch(ctx context.Context, src *dagger.Directory, eventTrigger *dagger.File, ghUsername, ghPassword *dagger.Secret) error {
	ci := dag.Pocketci(eventTrigger)

	switch {
	case ci.OnPullRequest() != nil:
		_, err := m.Test(ctx, src, ghUsername, ghPassword).Stdout(ctx)
		return err
	case ci.OnCommitPush() != nil:
		sha, err := ci.CommitPush().HeadCommit().Sha(ctx)
		if err != nil {
			return err
		}
		username, _ := ghUsername.Plaintext(ctx)
		_, err = m.Publish(ctx, src, "ghcr.io/franela/pocketci:"+sha, username, ghPassword)
		return err
	}

	return nil
}
```

You can alternatively parse the `eventTrigger` yourself. It is JSON file with the data contained [here](pocketci/server.go#L21). It will contain some metadata added by `pocketci` and the payload sent by the VCS.

### `Dispatch` interface

Pocketci uses a loosely defined interface for calling your module. This interface contains a single method called `Dispatch` that receives:
- `src`: the Directory containing the repository of the webhook
- `eventTrigger`: a file created by `pocketci` with metadata and the raw payload sent by the VCS
- an optional list of secrets configured by the user mapped to environment variables available in the context of `pocketci`'s agent.

This interface is simple and provides the most flexibility. However for certain cases it can be quite verbose. In pipelines you traditionally have separate workflow files for separate kind of triggers. We could take a similar approach, were we have a top level function for each trigger `pocketci` supports. That way we could have functions such as `OnPullRequest`, `OnCommitPush`, etc and make the call to the one that matches the event.

### `pocketci` module

The purpose of this module is to be a language agnostic way of interpreting the events that trigger the dagger call. At the moment this module is very basic and very specific to `GitHub`. But I believe it has the potential to hide the VCS from the implementation and help us achieve a trully portable CI experience. If this module provides the constructs for every event that could trigger dagger calls in a compatible way (but still providing the underlying raw data for corner cases) then users could easily migrate across VCS.

We can think of this module as the entrypoint for standardizing events in general. Not just VCS stuff.

### Multiple `pocketci.yaml` per repository?

At the moment `pocketci` only supports one specification per repository. And each spec supports only a single module. This is a limitation that we plan to change. If you have a mono-repo with many applications, using `pocketci` today would mean that a single `Dispatch` would be in charge of orchestrating the calls for all applications. This in turn makes every secret of every application available no matter which application is the one triggering the actual change and it will likely make for a very unmaintainable dagger function. We plan to change this but are unsure when, if you are interested you can open an issue to let us know.
