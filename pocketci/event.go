package pocketci

import (
	"context"
	"errors"
	"fmt"

	"dagger.io/dagger"
	"github.com/iancoleman/strcase"
)

// dispatcherFunction returns the function that should be called for the corresponding
// module given the vendor, eventType and filter that were specified.
// The anatomy of a function compatible with Pocketci is `On<Vendor><Event><Filter>`
// in addition to the top level `Dispatch` function. The funnel of function selection
// starts in the most fine-grained and finishes in `Dispatch`. For example, given
// vendor=github, eventType=pull_request and filter=opened the function will
// favour:
// 1. OnGithubPullRequestOpened
// 2. OnGithubPullRequest
// 3. OnGithub
// 4. Dispatch
// There are unit tests available with real dagger modules that make sure
// this logic remains true.
func dispatcherFunction(ctx context.Context, vendor, eventType, filter string, mod *dagger.Module) (string, string, error) {
	modName, err := mod.Name(ctx)
	if err != nil {
		return "", "", fmt.Errorf("could not get module name: %s", err)
	}
	modName = strcase.ToLowerCamel(modName)

	objects, err := mod.Objects(ctx)
	if err != nil {
		return "", "", fmt.Errorf("could not list module objects: %s", err)
	}

	for _, obj := range objects {
		object := obj.AsObject()
		if object == nil {
			continue
		}

		objName, err := object.Name(ctx)
		if err != nil {
			continue
		}

		objName = strcase.ToLowerCamel(objName)
		if objName != modName {
			continue
		}

		funcs, err := object.Functions(ctx)
		if err != nil {
			return "", "", fmt.Errorf("could not list functions from object %s: %s", objName, err)
		}

		functions := map[string]*dagger.Function{}
		for _, fn := range funcs {
			fnName, err := fn.Name(ctx)
			if err != nil {
				return "", "", fmt.Errorf("could not get function name for object %s: %s", objName, err)
			}
			functions[fnName] = &fn
		}

		var (
			vendorEventFilterMatch = strcase.ToLowerCamel(fmt.Sprintf("on-%s-%s-%s", vendor, eventType, filter))
			vendorEventMatch       = strcase.ToLowerCamel(fmt.Sprintf("on-%s-%s", vendor, eventType))
			vendorMatch            = strcase.ToLowerCamel(fmt.Sprintf("on-%s", vendor))
			dispatcherMatch        = "dispatch"
		)
		checks := []struct {
			name                              string
			withVendor, withEvent, withFilter bool
		}{
			{vendorEventFilterMatch, false, false, false},
			{vendorEventMatch, false, false, true},
			{vendorMatch, false, true, true},
			{dispatcherMatch, true, true, true},
		}
		for _, check := range checks {
			fn, ok := functions[check.name]
			if !ok {
				continue
			}
			valid, err := isValidSignature(ctx, fn, check.withVendor, check.withEvent, check.withFilter)
			if err != nil {
				return "", "", err
			}
			if !valid {
				return "", "", fmt.Errorf("%s is missing required arguments", vendorEventFilterMatch)
			}

			args := ""
			if check.withFilter {
				args = "--filter " + filter
			}
			if check.withEvent {
				args += " --event " + eventType
			}
			if check.withVendor {
				args += " --vendor " + vendor
			}

			return check.name, args, nil
		}
	}

	return "", "", errors.New("did not find dispatcher function nor main object")
}

func isValidSignature(ctx context.Context, fn *dagger.Function, withVendor, withEvent, withFilter bool) (bool, error) {
	fnName, _ := fn.Name(ctx)
	args, err := fn.Args(ctx)
	if err != nil {
		return false, fmt.Errorf("could not get args for function %s: %s", fnName, err)
	}

	var hasEventTrigger, hasSrc, hasVendor, hasEvent, hasFilter bool
	for _, arg := range args {
		argName, err := arg.Name(ctx)
		if err != nil {
			return false, fmt.Errorf("could not argument for function %s: %s", fnName, err)
		}

		if argName == "src" {
			hasSrc = true
		}
		if argName == "eventTrigger" {
			hasEventTrigger = true
		}
		if argName == "vendor" {
			hasVendor = true
		}
		if argName == "filter" {
			hasFilter = true
		}
		if argName == "event" {
			hasEvent = true
		}
	}

	// If withVendor, withEvent or withFilter are enabled then we want their corresponding
	// `has` to be true. Example truth table for `event`:
	// withEvent - hasEvent
	// 1			0		- 0
	// 1			1		- 1
	// 0			0		- 1
	// 0			1		- 1
	return hasSrc && hasEventTrigger && (!withVendor || hasVendor) && (!withEvent || hasEvent) && (!withFilter || hasFilter), nil
}
