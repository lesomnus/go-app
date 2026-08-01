# go-app

My flavor of "Hello, World!" for Go app.

## Quick Start

```sh
# Replace all occurrences of "github.com/lesomnus/go-app" with your own module path.
$ ./scripts/init.sh github.com/your-name/your-app

# Build and test the app.
# Build results will be placed in the `/dist` directory.
$ docker buildx bake build test

# Load apps into the local Docker engine.
$ docker buildx bake app --load
$ docker run --rm ghcr.io/lesomnus/go-app:local greet
> |........| 19:08:03.037 ○ 000000 000000 use default config
> Hello, hypnos!
```

## Configuration

The app reads `go-app.yaml`, or the file `--config` names. What it says can be
overruled, and the last word wins:

```
built-in defaults  <  the file  <  the environment  <  the flags
```

### From the environment

Every field answers to a variable named after the path to it: `format` of
`greet` is `GO_APP_GREET_FORMAT`, and so on for however deep the field sits.
`config` prints what the app ended up with, which is how to check.

```sh
$ GO_APP_GREET_FORMAT='Hi, %s.' go run . greet hypnos
> Hi, hypnos.
```

A name that starts with `GO_APP_` and answers to nothing is reported rather
than ignored in silence, since that is what a typo looks like:

```
$ GO_APP_GREET_FORMATT=x go run . greet hypnos
> ! no configuration is read from these, so they were ignored - env=[GO_APP_GREET_FORMATT]
```

The names are made from the configuration itself, so a field that is added
answers to one without anything being written here or anywhere else.

### From the environment, inside the file

A value in the file can name a variable instead of holding one, the way the
OpenTelemetry Collector spells it. This is for the parts of a value that must
not be written down, such as the password inside a connection string.

```yaml
greet:
  format: "${env:GREETING:-Hello}, %s!"
```

Without `:-default` the variable is required: one that is not set is an error
rather than an empty string, so a missing secret is noticed at startup instead
of at the first request. Write `$$` for a literal dollar sign.
