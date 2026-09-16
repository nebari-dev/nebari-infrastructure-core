## nic validate

Validate configuration file

### Synopsis

Validate the nebari-config.yaml file without deploying any infrastructure.
This command checks that the configuration file is properly formatted and contains
all required fields.

Configs that still contain the reserved CHANGEME placeholder are rejected, naming
every field left unfilled.

```
nic validate [flags]
```

### Options

```
  -f, --file string   Path to nebari-config.yaml file (auto-discovered if omitted)
  -h, --help          help for validate
```

### SEE ALSO

* [nic](nic.md)	 - Nebari Infrastructure Core - Cloud infrastructure management for Nebari

