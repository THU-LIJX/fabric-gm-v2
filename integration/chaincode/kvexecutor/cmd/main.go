/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package main

import (
	"fmt"
	"os"

	"github.com/VoneChain-CS/fabric-gm/integration/chaincode/kvexecutor"
	"github.com/VoneChain-CS/fabric-gm-chaincode-go/shim"
)

func main() {
	err := shim.Start(&kvexecutor.KVExcutor{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Exiting Simple chaincode: %s", err)
		os.Exit(2)
	}
}
