// +build pkcs11

/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package factory

import (
	"github.com/hyperledger/fabric/bccsp"
	"github.com/hyperledger/fabric/bccsp/pkcs11"
	"github.com/pkg/errors"
)

const pkcs11Enabled = false

// FactoryOpts holds configuration information used to initialize factory implementations
type FactoryOpts struct {
	Default string             `mapstructure:"default" json:"default" yaml:"Default"`
	SwOpts       *SwOpts            `mapstructure:"SW,omitempty" json:"SW,omitempty" yaml:"SwOpts"`
	Pkcs11Opts   *pkcs11.PKCS11Opts `mapstructure:"PKCS11,omitempty" json:"PKCS11,omitempty" yaml:"PKCS11"`
}

// InitFactories must be called before using factory interfaces
// It is acceptable to call with config = nil, in which case
// some defaults will get used
// Error is returned only if defaultBCCSP cannot be found
func InitFactories(config *FactoryOpts) error {
	factoriesInitOnce.Do(func() {
		factoriesInitError = initFactories(config)
	})

	return factoriesInitError
}

func initFactories(config *FactoryOpts) error {
	// Take some precautions on default opts
	if config == nil {
		config = GetDefaultOpts()
	}

	if config.Default == "" {
		config.Default = "GM"
	}

	if config.SwOpts == nil {
		config.SwOpts = GetDefaultOpts().SwOpts
	}

	// Software-Based BCCSP
	if config.Default == "GM" && config.SwOpts != nil {
		f := &GMFactory{}
		var err error
		defaultBCCSP, err = initBCCSP(f, config)
		if err != nil {
			return errors.Wrap(err, "Failed initializing SW.BCCSP")
		}
	}

	// Software-Based BCCSP
	if config.Default == "SW" && config.SwOpts != nil {
		f := &SWFactory{}
		var err error
		defaultBCCSP, err = initBCCSP(f, config)
		if err != nil {
			return errors.Wrap(err, "Failed initializing SW.BCCSP")
		}
	}

	// PKCS11-Based BCCSP
	if config.Default == "PKCS11" && config.Pkcs11Opts != nil {
		f := &PKCS11Factory{}
		var err error
		defaultBCCSP, err = initBCCSP(f, config)
		if err != nil {
			return errors.Wrapf(err, "Failed initializing PKCS11.BCCSP")
		}
	}

	if defaultBCCSP == nil {
		return errors.Errorf("Could not find default `%s` BCCSP", config.Default)
	}

	return nil
}

// GetBCCSPFromOpts returns a BCCSP created according to the options passed in input.
func GetBCCSPFromOpts(config *FactoryOpts) (bccsp.BCCSP, error) {
	var f BCCSPFactory
	switch config.Default {
	case "GM":
		f = &GMFactory{}
	case "SW":
		f = &SWFactory{}
	case "PKCS11":
		f = &PKCS11Factory{}
	default:
		return nil, errors.Errorf("Could not find BCCSP, no '%s' provider", config.Default)
	}

	csp, err := f.Get(config)
	if err != nil {
		return nil, errors.Wrapf(err, "Could not initialize BCCSP %s", f.Name())
	}
	return csp, nil
}
