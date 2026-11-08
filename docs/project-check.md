# Check a local project's compatibility

Run `projectcheck` before an offline car or energy command:

```sh
go run ./cmd/projectcheck -project project.json
```

Use `go run ./cmd/projectcheck -h` for help.
The command requires one `-project` path.
It rejects positional arguments and unsupported flags.
It does not write or convert the input file.

A valid project produces a quoted name, project version, station count, lane count, and pod count.
The command exits successfully after the native checks pass.
After the summary, a `Warning:` line names each lane whose [large-class tail](model.md#vehicle-classes) exceeds its length, up to 5 lanes, and one more line counts the others.
A warning does not change the exit status.
Before the summary, the command checks the parking routes.
For each class of the fleet that has a parking goal, each passenger berth that accepts the class needs a route to a parking goal and a route from one, for a pod of that class.
A parking goal is a berth of a parking station that accepts the class, or the entry of a depot that accepts it.
Without such a route, an idle pod of the class cannot leave the berth for parking when another pod needs the berth.
A gap is an error, and the error gives the count of the berths with a gap and names the first.
An argument, read, decode, validation, parking route, or summary write error produces a nonzero exit.
Errors identify the failed stage.

The reader accepts at most 32 MiB, the local project limit.
The command decodes the file as the server does: as `serve -project` reads its file and as the session project command reads its project.
It then runs `project.Validate`.
Member names must match exactly.
A name that differs from a declared name only in case is an unknown member.
The command rejects unknown members and a second JSON value.
When a name repeats in one object, the last value applies, as in the server.
It retains native version, class, bank, and null rules.
Historical optional null values keep their existing meaning.
It does not add a separate browser validator or new project defaults.

These checks cover static project compatibility, including native fleet admission and geometry rules.
They do not advance simulation ticks, generate demand, or open a network connection.
Success does not qualify dynamic service, authored speeds, or operating byte limits.
The offline command still validates its project and its separate car plan or energy profiles.

See [offline workflow](offline-workflow.md) for input authoring and report instructions.
