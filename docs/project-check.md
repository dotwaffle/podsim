# Check a local project's compatibility

Run `projectcheck` before an offline car or energy command:

```sh
go run ./cmd/projectcheck -project project.json
```

Use `go run ./cmd/projectcheck -h` for help.
The command requires one `-project` path.
It rejects positional arguments and unsupported flags.
It does not write or convert the input file.

A valid project produces a quoted name, project version, station count, lane count, and fleet count.
The command exits successfully after the native checks pass.
An argument, read, decode, validation, or summary write error produces a nonzero exit.
Errors identify the failed stage.

The reader accepts at most 10 MiB, the existing local project limit.
The command uses the native JSON decoder and `project.Validate`.
It rejects unknown and duplicate members.
It retains native version, class, bank, and null rules.
Historical optional null values keep their existing meaning.
It does not add a separate browser validator or new project defaults.

These checks cover static project compatibility, including native fleet admission and geometry rules.
They do not advance simulation ticks, generate demand, or open a network connection.
Success does not qualify dynamic service, authored speeds, or operating byte limits.
The offline command still validates its project and its separate car plan or energy profiles.

See [offline workflow](offline-workflow.md) for input authoring and report instructions.
