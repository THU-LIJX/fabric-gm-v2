/*
Copyright Suzhou Tongji Fintech Research Institute 2017 All Rights Reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package gm

import (
	"fmt"

	"github.com/hyperledger/fabric/bccsp"
	"github.com/tjfoc/gmsm/sm2"
)

//定义国密SM2 keygen 结构体，实现 KeyGenerator 接口
type SM2KeyGenerator struct {
}

func (gm *SM2KeyGenerator) KeyGen(opts bccsp.KeyGenOpts) (k bccsp.Key, err error) {
  logger.Infof("bccsp gm gmsm2KeyGenerator KeyGen")
	privKey, err := sm2.GenerateKey()
	if err != nil {
		return nil, fmt.Errorf("Failed generating GMSM2 key  [%s]", err)
	}

	return &SM2PrivateKey{privKey}, nil
}


//定义国密SM4 keygen 结构体，实现 KeyGenerator 接口
type SM4KeyGenerator struct {
	length int
}

func (gm *SM4KeyGenerator) KeyGen(opts bccsp.KeyGenOpts) (k bccsp.Key, err error) {
  logger.Infof("bccsp gm gmsm4KeyGenerator KeyGen")
	lowLevelKey, err := GetRandomBytes(int(gm.length))
	if err != nil {
		return nil, fmt.Errorf("Failed generating GMSM4 %d key [%s]", gm.length, err)
	}

	return &SM4PrivateKey{lowLevelKey, false}, nil
}
