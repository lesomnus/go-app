package config

type ServerConfig struct {
	// Addr is the address the gRPC server listens on, e.g. ":50051".
	Addr string `yaml:"addr"`

	TLS TLSConfig `yaml:"tls"`
}
