# webhook

`webhook` is a dagger module that wraps the [webhook]() project to provide a simple replacement to your CI runners. Configure your repository with a `hooks.json`, run this dagger module and connect your VCS to it.

### TODO

- [ ] Install dagger in webhook container and validate that webhook container that can run commands from the dagger module of the repository
- [ ] Integrate with real webhooks through a tunnel
- [ ] Add support for branching
- [ ] Add support for Gitlab
- [ ] Figure out how to handle concurrency since we currently can process one request at the time as we're sharing the same repo folder for all requests and the `gitCloneProxy` method will constantly override that directory
