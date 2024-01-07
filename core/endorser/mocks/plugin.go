/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

// Code generated by mockery v1.0.0
package mocks

import endorsement "github.com/VoneChain-CS/fabric-gm/core/handlers/endorsement/api"
import mock "github.com/stretchr/testify/mock"
import peer "github.com/VoneChain-CS/fabric-gm-protos-go/peer"

// Plugin is an autogenerated mock type for the Plugin type
type Plugin struct {
	mock.Mock
}

// Endorse provides a mock function with given fields: payload, sp
func (_m *Plugin) Endorse(payload []byte, sp *peer.SignedProposal) (*peer.Endorsement, []byte, error) {
	ret := _m.Called(payload, sp)

	var r0 *peer.Endorsement
	if rf, ok := ret.Get(0).(func([]byte, *peer.SignedProposal) *peer.Endorsement); ok {
		r0 = rf(payload, sp)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*peer.Endorsement)
		}
	}

	var r1 []byte
	if rf, ok := ret.Get(1).(func([]byte, *peer.SignedProposal) []byte); ok {
		r1 = rf(payload, sp)
	} else {
		if ret.Get(1) != nil {
			r1 = ret.Get(1).([]byte)
		}
	}

	var r2 error
	if rf, ok := ret.Get(2).(func([]byte, *peer.SignedProposal) error); ok {
		r2 = rf(payload, sp)
	} else {
		r2 = ret.Error(2)
	}

	return r0, r1, r2
}

// Init provides a mock function with given fields: dependencies
func (_m *Plugin) Init(dependencies ...endorsement.Dependency) error {
	_va := make([]interface{}, len(dependencies))
	for _i := range dependencies {
		_va[_i] = dependencies[_i]
	}
	var _ca []interface{}
	_ca = append(_ca, _va...)
	ret := _m.Called(_ca...)

	var r0 error
	if rf, ok := ret.Get(0).(func(...endorsement.Dependency) error); ok {
		r0 = rf(dependencies...)
	} else {
		r0 = ret.Error(0)
	}

	return r0
}
