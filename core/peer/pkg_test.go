/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package peer_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io/ioutil"
	"net"
	"path/filepath"
	"testing"
	"time"

	configtxtest "github.com/hyperledger/fabric/common/configtx/test"
	"github.com/hyperledger/fabric/core/ledger/mock"
	"github.com/hyperledger/fabric/core/peer"
	"github.com/hyperledger/fabric/internal/pkg/comm"
	"github.com/hyperledger/fabric/internal/pkg/comm/testpb"
	"github.com/hyperledger/fabric/internal/pkg/txflags"
	"github.com/hyperledger/fabric/msp"
	"github.com/golang/protobuf/proto"
	cb "github.com/hyperledger/fabric-protos-go/common"
	mspproto "github.com/hyperledger/fabric-protos-go/msp"
	pb "github.com/hyperledger/fabric-protos-go/peer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// default timeout for grpc connections
var timeout = time.Second * 1

// test server to be registered with the GRPCServer
type testServiceServer struct{}

func (tss *testServiceServer) EmptyCall(context.Context, *testpb.Empty) (*testpb.Empty, error) {
	return new(testpb.Empty), nil
}

// createCertPool creates an x509.CertPool from an array of PEM-encoded certificates
func createCertPool(rootCAs [][]byte) (*x509.CertPool, error) {
	certPool := x509.NewCertPool()
	for _, rootCA := range rootCAs {
		if !certPool.AppendCertsFromPEM(rootCA) {
			return nil, errors.New("Failed to load root certificates")
		}
	}
	return certPool, nil
}

// helper function to invoke the EmptyCall againt the test service
func invokeEmptyCall(address string, dialOptions []grpc.DialOption) (*testpb.Empty, error) {
	//add DialOptions
	dialOptions = append(dialOptions, grpc.WithBlock())
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	//create GRPC client conn
	clientConn, err := grpc.DialContext(ctx, address, dialOptions...)
	if err != nil {
		return nil, err
	}
	defer clientConn.Close()

	//create GRPC client
	client := testpb.NewTestServiceClient(clientConn)

	//invoke service
	empty, err := client.EmptyCall(context.Background(), new(testpb.Empty))
	if err != nil {
		return nil, err
	}

	return empty, nil
}

// helper function to build an MSPConfig given root certs
func createMSPConfig(rootCerts, tlsRootCerts, tlsIntermediateCerts [][]byte,
	mspID string) (*mspproto.MSPConfig, error) {

	fmspconf := &mspproto.FabricMSPConfig{
		RootCerts:            rootCerts,
		TlsRootCerts:         tlsRootCerts,
		TlsIntermediateCerts: tlsIntermediateCerts,
		Name:                 mspID,
		FabricNodeOus: &mspproto.FabricNodeOUs{
			Enable: true,
			ClientOuIdentifier: &mspproto.FabricOUIdentifier{
				OrganizationalUnitIdentifier: "client",
			},
			PeerOuIdentifier: &mspproto.FabricOUIdentifier{
				OrganizationalUnitIdentifier: "peer",
			},
			AdminOuIdentifier: &mspproto.FabricOUIdentifier{
				OrganizationalUnitIdentifier: "admin",
			},
		},
	}

	fmpsjs, err := proto.Marshal(fmspconf)
	if err != nil {
		return nil, err
	}
	mspconf := &mspproto.MSPConfig{Config: fmpsjs, Type: int32(msp.FABRIC)}
	return mspconf, nil
}

func createConfigBlock(channelID string, appMSPConf, ordererMSPConf *mspproto.MSPConfig,
	appOrgID, ordererOrgID string) (*cb.Block, error) {
	block, err := configtxtest.MakeGenesisBlockFromMSPs(channelID, appMSPConf, ordererMSPConf, appOrgID, ordererOrgID)
	if block == nil || err != nil {
		return block, err
	}

	txsFilter := txflags.NewWithValues(len(block.Data.Data), pb.TxValidationCode_VALID)
	block.Metadata.Metadata[cb.BlockMetadataIndex_TRANSACTIONS_FILTER] = txsFilter

	return block, nil
}

func TestUpdateRootsFromConfigBlock(t *testing.T) {
	// load test certs from testdata
	org1CA, err := ioutil.ReadFile(filepath.Join("testdata", "Org1-cert.pem"))
	require.NoError(t, err)
	org1Server1Key, err := ioutil.ReadFile(filepath.Join("testdata", "Org1-server1-key.pem"))
	require.NoError(t, err)
	org1Server1Cert, err := ioutil.ReadFile(filepath.Join("testdata", "Org1-server1-cert.pem"))
	require.NoError(t, err)
	org2CA, err := ioutil.ReadFile(filepath.Join("testdata", "Org2-cert.pem"))
	require.NoError(t, err)
	org2Server1Key, err := ioutil.ReadFile(filepath.Join("testdata", "Org2-server1-key.pem"))
	require.NoError(t, err)
	org2Server1Cert, err := ioutil.ReadFile(filepath.Join("testdata", "Org2-server1-cert.pem"))
	require.NoError(t, err)
	org2IntermediateCA, err := ioutil.ReadFile(filepath.Join("testdata", "Org2-child1-cert.pem"))
	require.NoError(t, err)
	org2IntermediateServer1Key, err := ioutil.ReadFile(filepath.Join("testdata", "Org2-child1-server1-key.pem"))
	require.NoError(t, err)
	org2IntermediateServer1Cert, err := ioutil.ReadFile(filepath.Join("testdata", "Org2-child1-server1-cert.pem"))
	require.NoError(t, err)
	ordererOrgCA, err := ioutil.ReadFile(filepath.Join("testdata", "Org3-cert.pem"))
	require.NoError(t, err)
	ordererOrgServer1Key, err := ioutil.ReadFile(filepath.Join("testdata", "Org3-server1-key.pem"))
	require.NoError(t, err)
	ordererOrgServer1Cert, err := ioutil.ReadFile(filepath.Join("testdata", "Org3-server1-cert.pem"))
	require.NoError(t, err)

	// create test MSPConfigs
	org1MSPConf, err := createMSPConfig([][]byte{org2CA}, [][]byte{org1CA}, [][]byte{}, "Org1MSP")
	require.NoError(t, err)
	org2MSPConf, err := createMSPConfig([][]byte{org1CA}, [][]byte{org2CA}, [][]byte{}, "Org2MSP")
	require.NoError(t, err)
	org2IntermediateMSPConf, err := createMSPConfig([][]byte{org1CA}, [][]byte{org2CA}, [][]byte{org2IntermediateCA}, "Org2IntermediateMSP")
	require.NoError(t, err)
	ordererOrgMSPConf, err := createMSPConfig([][]byte{org1CA}, [][]byte{ordererOrgCA}, [][]byte{}, "OrdererOrgMSP")
	require.NoError(t, err)

	// create test channel create blocks
	channel1Block, err := createConfigBlock("channel1", org1MSPConf, ordererOrgMSPConf, "Org1MSP", "OrdererOrgMSP")
	require.NoError(t, err)
	channel2Block, err := createConfigBlock("channel2", org2MSPConf, ordererOrgMSPConf, "Org2MSP", "OrdererOrgMSP")
	require.NoError(t, err)
	channel3Block, err := createConfigBlock("channel3", org2IntermediateMSPConf, ordererOrgMSPConf, "Org2IntermediateMSP", "OrdererOrgMSP")
	require.NoError(t, err)

	serverConfig := comm.ServerConfig{
		SecOpts: comm.SecureOptions{
			UseTLS:            true,
			Certificate:       org1Server1Cert,
			Key:               org1Server1Key,
			ServerRootCAs:     [][]byte{org1CA},
			RequireClientCert: true,
		},
	}

	peerInstance, cleanup := peer.NewTestPeer(t)
	defer cleanup()
	peerInstance.CredentialSupport = comm.NewCredentialSupport()

	createChannel := func(t *testing.T, cid string, block *cb.Block) {
		err = peerInstance.CreateChannel(cid, block, &mock.DeployedChaincodeInfoProvider{}, nil, nil)
		if err != nil {
			t.Fatalf("Failed to create config block (%s)", err)
		}
		t.Logf("Channel %s MSPIDs: (%s)", cid, peerInstance.GetMSPIDs(cid))
	}

	org1CertPool, err := createCertPool([][]byte{org1CA})
	require.NoError(t, err)

	// use server cert as client cert
	org1ClientCert, err := tls.X509KeyPair(org1Server1Cert, org1Server1Key)
	require.NoError(t, err)

	org1Creds := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{org1ClientCert},
		RootCAs:      org1CertPool,
	})

	org2ClientCert, err := tls.X509KeyPair(org2Server1Cert, org2Server1Key)
	require.NoError(t, err)
	org2Creds := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{org2ClientCert},
		RootCAs:      org1CertPool,
	})

	org2IntermediateClientCert, err := tls.X509KeyPair(org2IntermediateServer1Cert, org2IntermediateServer1Key)
	require.NoError(t, err)
	org2IntermediateCreds := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{org2IntermediateClientCert},
		RootCAs:      org1CertPool,
	})

	ordererOrgClientCert, err := tls.X509KeyPair(ordererOrgServer1Cert, ordererOrgServer1Key)
	require.NoError(t, err)

	ordererOrgCreds := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{ordererOrgClientCert},
		RootCAs:      org1CertPool,
	})

	// basic function tests
	var tests = []struct {
		name          string
		serverConfig  comm.ServerConfig
		createChannel func(*testing.T)
		goodOptions   []grpc.DialOption
		badOptions    []grpc.DialOption
		numAppCAs     int
		numOrdererCAs int
	}{
		{
			name:          "MutualTLSOrg1Org1",
			serverConfig:  serverConfig,
			createChannel: func(t *testing.T) { createChannel(t, "channel1", channel1Block) },
			goodOptions:   []grpc.DialOption{grpc.WithTransportCredentials(org1Creds)},
			badOptions:    []grpc.DialOption{grpc.WithTransportCredentials(ordererOrgCreds)},
			numAppCAs:     3, // each channel also has a DEFAULT MSP
			numOrdererCAs: 1,
		},
		{
			name:          "MutualTLSOrg1Org2",
			serverConfig:  serverConfig,
			createChannel: func(t *testing.T) { createChannel(t, "channel2", channel2Block) },
			goodOptions: []grpc.DialOption{
				grpc.WithTransportCredentials(org2Creds),
			},
			badOptions: []grpc.DialOption{
				grpc.WithTransportCredentials(ordererOrgCreds),
			},
			numAppCAs:     6,
			numOrdererCAs: 2,
		},
		{
			name:          "MutualTLSOrg1Org2Intermediate",
			serverConfig:  serverConfig,
			createChannel: func(t *testing.T) { createChannel(t, "channel3", channel3Block) },
			goodOptions: []grpc.DialOption{
				grpc.WithTransportCredentials(org2IntermediateCreds),
			},
			badOptions: []grpc.DialOption{
				grpc.WithTransportCredentials(ordererOrgCreds),
			},
			numAppCAs:     10,
			numOrdererCAs: 3,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Logf("Running test %s ...", test.name)
			server, err := comm.NewGRPCServer("localhost:0", test.serverConfig)
			if err != nil {
				t.Fatalf("NewGRPCServer failed with error [%s]", err)
				return
			}

			peerInstance.SetServer(server)
			peerInstance.ServerConfig = test.serverConfig

			assert.NoError(t, err, "NewGRPCServer should not have returned an error")
			assert.NotNil(t, server, "NewGRPCServer should have created a server")
			// register a GRPC test service
			testpb.RegisterTestServiceServer(server.Server(), &testServiceServer{})
			go server.Start()
			defer server.Stop()

			// extract dynamic listen port
			_, port, err := net.SplitHostPort(server.Listener().Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("listenAddress: %s", server.Listener().Addr())
			testAddress := "localhost:" + port
			t.Logf("testAddress: %s", testAddress)

			// invoke the EmptyCall service with good options but should fail
			// until channel is created and root CAs are updated
			_, err = invokeEmptyCall(testAddress, test.goodOptions)
			assert.Error(t, err, "Expected error invoking the EmptyCall service ")

			// creating channel should update the trusted client roots
			test.createChannel(t)

			// invoke the EmptyCall service with good options
			_, err = invokeEmptyCall(testAddress, test.goodOptions)
			assert.NoError(t, err, "Failed to invoke the EmptyCall service")

			// invoke the EmptyCall service with bad options
			_, err = invokeEmptyCall(testAddress, test.badOptions)
			assert.Error(t, err, "Expected error using bad dial options")
		})
	}
}
