package server

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"time"
)

type opampTransportMode uint8

const (
	opampTransportDisabled opampTransportMode = iota
	opampTransportInsecure
	opampTransportTLS
)

type opampTransport struct {
	mode      opampTransportMode
	tlsConfig *tls.Config
}

var (
	errOpAMPTransportMissingTLS  = errors.New("TLS certificate and key are required")
	errOpAMPTransportPartialTLS  = errors.New("TLS certificate and key must both be configured")
	errOpAMPTransportConflict    = errors.New("plaintext and TLS transports cannot both be configured")
	errOpAMPTransportSelector    = errors.New("insecure transport selector must be exactly true or false")
	errOpAMPTransportInvalidPair = errors.New("TLS certificate or key is invalid")
	errOpAMPTransportInvalidLeaf = errors.New("TLS leaf certificate is invalid")
)

func resolveOpAMPTransport(cfg Config, now time.Time) (opampTransport, error) {
	insecure := cfg.OpAMPInsecure
	if insecure == "" {
		insecure = "false"
	}
	if insecure != "true" && insecure != "false" {
		return opampTransport{}, errOpAMPTransportSelector
	}

	hasCert := cfg.OpAMPTLSCertFile != ""
	hasKey := cfg.OpAMPTLSKeyFile != ""
	if insecure == "true" {
		if hasCert || hasKey {
			return opampTransport{}, errOpAMPTransportConflict
		}
		return opampTransport{mode: opampTransportInsecure}, nil
	}
	if !hasCert && !hasKey {
		return opampTransport{}, errOpAMPTransportMissingTLS
	}
	if !hasCert || !hasKey {
		return opampTransport{}, errOpAMPTransportPartialTLS
	}

	certificate, err := tls.LoadX509KeyPair(cfg.OpAMPTLSCertFile, cfg.OpAMPTLSKeyFile)
	if err != nil {
		return opampTransport{}, errOpAMPTransportInvalidPair
	}
	if len(certificate.Certificate) == 0 {
		return opampTransport{}, errOpAMPTransportInvalidLeaf
	}
	leaf, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil || now.Before(leaf.NotBefore) || !now.Before(leaf.NotAfter) {
		return opampTransport{}, errOpAMPTransportInvalidLeaf
	}
	certificate.Leaf = leaf

	return opampTransport{
		mode: opampTransportTLS,
		tlsConfig: &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{certificate},
		},
	}, nil
}
