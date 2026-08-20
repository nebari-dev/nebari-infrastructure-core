package local

import "reflect"

// ConfigType reports the Go type of this provider's configuration struct
// (the optional repository.ConfigTyped capability used by schema generation
// and config scaffolding).
func (p *Provider) ConfigType() reflect.Type { return reflect.TypeFor[Config]() }
