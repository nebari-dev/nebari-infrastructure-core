package azure

import "reflect"

// ConfigType returns the reflect.Type of this provider's configuration struct,
// so schema-generation tooling (cmd/schemagen) can reflect on it via the
// registry without importing this package directly.
func (p *Provider) ConfigType() reflect.Type { return reflect.TypeFor[Config]() }
