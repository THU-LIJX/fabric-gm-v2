// Code generated by mockery v1.0.0. DO NOT EDIT.

package mocks

import (
	common "github.com/VoneChain-CS/fabric-gm-protos-go/common"
	cluster "github.com/VoneChain-CS/fabric-gm/orderer/common/cluster"

	mock "github.com/stretchr/testify/mock"
)

// VerifierFactory is an autogenerated mock type for the VerifierFactory type
type VerifierFactory struct {
	mock.Mock
}

// VerifierFromConfig provides a mock function with given fields: configuration, channel
func (_m *VerifierFactory) VerifierFromConfig(configuration *common.ConfigEnvelope, channel string) (cluster.BlockVerifier, error) {
	ret := _m.Called(configuration, channel)

	var r0 cluster.BlockVerifier
	if rf, ok := ret.Get(0).(func(*common.ConfigEnvelope, string) cluster.BlockVerifier); ok {
		r0 = rf(configuration, channel)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(cluster.BlockVerifier)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(*common.ConfigEnvelope, string) error); ok {
		r1 = rf(configuration, channel)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}
