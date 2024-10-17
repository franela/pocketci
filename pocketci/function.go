package pocketci

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"dagger.io/dagger"
	"github.com/iancoleman/strcase"
)

var ErrNoFunctionsMatched = errors.New("did not find dispatcher function nor main object")

type Function struct {
	Name string
	Args string
}

func hasFunction(ctx context.Context, mod *dagger.Module, functions ...string) (string, error) {
	modName, err := mod.Name(ctx)
	if err != nil {
		return "", fmt.Errorf("could not get module name: %s", err)
	}
	modName = strcase.ToLowerCamel(modName)

	funcs := map[string]bool{}
	for _, fn := range functions {
		funcs[fn] = true
	}

	objects, err := mod.Objects(ctx)
	if err != nil {
		return "", fmt.Errorf("could not list module objects: %s", err)
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

		functions, err := object.Functions(ctx)
		if err != nil {
			return "", fmt.Errorf("could not list functions from object %s: %s", objName, err)
		}

		for _, fn := range functions {
			fnName, err := fn.Name(ctx)
			if err != nil {
				return "", fmt.Errorf("could not get function name for object %s: %s", objName, err)
			}

			if funcs[fnName] {
				return fnName, nil
			}
		}
	}

	return "", ErrNoFunctionsMatched
}

func matchFunctions(ctx context.Context, vendor, eventType, filter string, changes []string, mod *dagger.Module) ([]Function, error) {
	modName, err := mod.Name(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not get module name: %s", err)
	}
	modName = strcase.ToLowerCamel(modName)

	objects, err := mod.Objects(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not list module objects: %s", err)
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
			return nil, fmt.Errorf("could not list functions from object %s: %s", objName, err)
		}

		moduleFunctions := map[string]*dagger.Function{}
		for _, fn := range funcs {
			fnName, err := fn.Name(ctx)
			if err != nil {
				return nil, fmt.Errorf("could not get function name for object %s: %s", objName, err)
			}
			moduleFunctions[fnName] = &fn
		}

		var (
			vendorEventFilterMatch = strcase.ToLowerCamel(fmt.Sprintf("on-%s-%s-%s", vendor, eventType, filter))
			vendorEventMatch       = strcase.ToLowerCamel(fmt.Sprintf("on-%s-%s", vendor, eventType))
			vendorMatch            = strcase.ToLowerCamel(fmt.Sprintf("on-%s", vendor))
			dispatcherMatch        = "dispatch"
			functions              = []Function{}
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
			for fnName, fn := range moduleFunctions {
				if fnName != check.name && !strings.HasSuffix(fnName, strcase.ToCamel(check.name)) {
					continue
				}
				fnName = strcase.ToKebab(fnName)

				args, err := fn.Args(ctx)
				if err != nil {
					return nil, fmt.Errorf("could not get args for function %s: %s", fnName, err)
				}

				var (
					hasEventTrigger, hasSrc  bool
					hasEvent, matchEvent     bool
					hasFilter, matchFilter   bool
					hasVendor, matchVendor   bool
					hasChanges, matchChanges bool
				)
				for _, arg := range args {
					argName, err := arg.Name(ctx)
					if err != nil {
						return nil, fmt.Errorf("could not argument for function %s: %s", fnName, err)
					}

					// if the argument `matchChanges` is present we need to
					// match whether the list of files that changed should generate
					// a trigger for this function
					if argName == "onChanges" {
						hasChanges = true
						defaultValue, err := arg.DefaultValue(ctx)
						if err != nil {
							return nil, fmt.Errorf("could not get default value for %s on function %s: %s", argName, fnName, err)
						}
						patterns := strings.Split(strings.ReplaceAll(string(defaultValue), `"`, ""), ",")
						if len(patterns) != 0 && !Match(changes, patterns...) {
							slog.Info("`on-changes` do not match files changed", slog.String("function", fnName),
								slog.String("on-changes", string(defaultValue)), slog.String("changes", strings.Join(changes, ",")))
							continue
						}
						matchChanges = true
					}

					if argName == "src" {
						hasSrc = true
					}
					if argName == "eventTrigger" {
						hasEventTrigger = true
					}

					var required string
					if argName == "vendor" {
						hasVendor = true
						matchVendor, required, err = matchArg(ctx, fnName, arg, vendor)
						if err != nil {
							return nil, err
						}

						if !matchVendor {
							slog.Info("`vendor` value does not match current vendor", slog.String("function", fnName),
								slog.String("required-vendor", required),
								slog.String("current-vendor", vendor))
							continue
						}
					}

					if argName == "filter" {
						hasFilter = true

						matchFilter, required, err = matchArg(ctx, fnName, arg, filter)
						if err != nil {
							return nil, err
						}

						if !matchFilter {
							slog.Info("`filter` value does not match current filter", slog.String("function", fnName),
								slog.String("required-filter", required),
								slog.String("current-filter", filter))
							continue
						}
					}
					if argName == "event" {
						hasEvent = true

						matchEvent, required, err = matchArg(ctx, fnName, arg, eventType)
						if err != nil {
							return nil, err
						}

						if !matchEvent {
							slog.Info("`event` value does not match current event", slog.String("function", fnName),
								slog.String("required-event", required),
								slog.String("current-event", eventType))
							continue
						}
					}
				}

				if hasChanges && !matchChanges {
					continue
				}

				if hasVendor && !matchVendor {
					continue
				}

				if hasEvent && !matchEvent {
					continue
				}

				if hasFilter && !matchFilter {
					continue
				}

				argChecks := []struct {
					hasArg bool
					err    error
				}{
					{hasSrc, fmt.Errorf("%s is missing the `src *dagger.Directory` argument", fnName)},
					{hasEventTrigger, fmt.Errorf("%s is missing the `event-trigger *dagger.File` argument", fnName)},
					{!check.withVendor || hasVendor, fmt.Errorf("%s is missing the `vendor string` argument", fnName)},
					{!check.withEvent || hasEvent, fmt.Errorf("%s is missing the `event string` argument", fnName)},
					{!check.withFilter || hasFilter, fmt.Errorf("%s is missing the `filter string` argument", fnName)},
				}
				for _, argCheck := range argChecks {
					if argCheck.hasArg {
						continue
					}
					return nil, argCheck.err
				}

				function := Function{Name: fnName}
				if check.withFilter {
					function.Args = "--filter " + filter
				}
				if check.withEvent {
					function.Args += " --event " + eventType
				}
				if check.withVendor {
					function.Args += " --vendor " + vendor
				}
				if hasChanges && matchChanges {
					function.Args += " --on-changes " + strings.Join(changes, ",")
				}

				functions = append(functions, function)
			}

			if len(functions) != 0 {
				return functions, nil
			}
		}
	}
	return nil, ErrNoFunctionsMatched
}

func matchArg(ctx context.Context, fnName string, arg dagger.FunctionArg, value string) (bool, string, error) {
	argName, _ := arg.Name(ctx)
	defaultValue, err := arg.DefaultValue(ctx)
	if err != nil {
		return false, string(defaultValue), fmt.Errorf("could not get default value for %s on function %s: %s", argName, fnName, err)
	}

	values := strings.Split(strings.ReplaceAll(string(defaultValue), `"`, ""), ",")
	return defaultValue == "" || slices.Contains(values, value), "", nil
}
