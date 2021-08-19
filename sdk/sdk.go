/*
 * Copyright (C) 2021 The ontology Authors
 * This file is part of The ontology library.
 *
 * The ontology is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Lesser General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * The ontology is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU Lesser General Public License for more details.
 *
 * You should have received a copy of the GNU Lesser General Public License
 * along with The ontology.  If not, see <http://www.gnu.org/licenses/>.
 */
package sdk

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"ontlogin-sdk-go/did"
	"ontlogin-sdk-go/modules"
)

type SDKConfig struct {
	chain       []string
	alg         []string
	serverInfo  *modules.ServerInfo
	vcfilters   []*modules.VCFilter
	trustedDIDs []string
}

type OntLoginSdk struct {
	didResolvers map[string]did.DidResolver
	conf         *SDKConfig
	//this function should generate and save a 128-uuid for client
	genRandomNonceFunc func() string
	//this function check the client response nonce is generated by server or not
	checkNonceExistFunc func(string) error
}

func NewOntLoginSdk(conf *SDKConfig, resolvers map[string]did.DidResolver, nonceFunc func() string, checkNonceFunc func(string) error) (*OntLoginSdk, error) {
	return &OntLoginSdk{
		didResolvers:        resolvers,
		conf:                conf,
		genRandomNonceFunc:  nonceFunc,
		checkNonceExistFunc: checkNonceFunc,
	}, nil
}

func (s *OntLoginSdk) GetDIDChain(did string) (string, error) {
	tmpArr := strings.Split(did, ":")
	if len(tmpArr) != 3 {
		return "", fmt.Errorf("valid did format")
	}
	return tmpArr[1], nil
}

func (s *OntLoginSdk) GenerateChallenge(req *modules.ClientHello) (*modules.ServerHello, error) {

	//1. validate req
	if err := s.validateClientHello(req); err != nil {
		return nil, err
	}
	//2. generate uuid
	uuid := s.genRandomNonceFunc()

	res := &modules.ServerHello{}
	res.Ver = modules.SYS_VER
	res.Type = modules.TYPE_SERVER_HELLO
	res.Nonce = uuid
	res.Server = s.conf.serverInfo
	res.Chain = s.conf.chain
	res.Alg = s.conf.alg

	if req.Action == modules.ACTION_REGISTER {
		res.VCFilters = s.conf.vcfilters
	}
	//serverproof
	//extension
	return res, nil
}

func (s *OntLoginSdk) GetCredentailJson(chain, presentation string) ([]string, error) {
	resolver, ok := s.didResolvers[chain]
	if !ok {
		return nil, fmt.Errorf("chain not supported")
	}

	return resolver.GetCredentialJsons(presentation)
}

func (s *OntLoginSdk) ValidateClientResponse(res *modules.ClientResponse) error {

	//1. validate res
	if err := s.validateClientResponse(res); err != nil {
		return err
	}

	did, index, err := getDIDKeyAndIndex(res.Proof.VerificationMethod)
	if !strings.EqualFold(did, res.Did) {
		return fmt.Errorf("did and VerificationMethod not match")
	}
	chain, err := s.GetDIDChain(did)
	if err != nil {
		return err
	}
	if err = s.checkNonceExistFunc(res.Proof.Nonce); err != nil {
		return fmt.Errorf("nonce is existed on server side")
	}
	msg := &modules.ClientResponseMsg{
		Type: res.Type,
		Server: modules.ServerInfoToSign{
			Name: s.conf.serverInfo.Name,
			Url:  s.conf.serverInfo.Url,
			Did:  s.conf.serverInfo.Did,
		},
		Did:     did,
		Created: res.Proof.Created,
	}

	sigdata, err := hex.DecodeString(res.Proof.Value)
	if err != nil {
		return fmt.Errorf("decode proof value failed:%s", err.Error())
	}
	dataToSign, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message failed:%s", err.Error())
	}
	resolver, ok := s.didResolvers[chain]
	if !ok {
		return fmt.Errorf("not a support did chain:%s", chain)
	}
	if err = resolver.VerifySig(did, index, dataToSign, sigdata); err != nil {
		return err
	}

	//verify presentation
	if res.VPs != nil && len(res.VPs) > 0 {

		requiredTypes := s.getRequiredVcTypes()
		for _, vp := range res.VPs {
			if err = resolver.VerifyPresentation(did, index, vp, s.conf.trustedDIDs, requiredTypes); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *OntLoginSdk) validateClientHello(req *modules.ClientHello) error {

	if !strings.EqualFold(req.Ver , modules.SYS_VER){
		return fmt.Errorf(modules.ERR_WRONG_VERSION)
	}
	if !strings.EqualFold(req.Type, modules.TYPE_CLIENT_HELLO){
		return fmt.Errorf(modules.ERR_TYPE_NOT_SUPPORTED)
	}
	if !strings.EqualFold(req.Action,"0") && !strings.EqualFold(req.Action,"1"){
		return fmt.Errorf(modules.ERR_ACTION_NOT_SUPPORTED)
	}

	return nil
}

func (s *OntLoginSdk) validateClientResponse(response *modules.ClientResponse) error {
	if !strings.EqualFold(response.Ver , modules.SYS_VER){
		return fmt.Errorf(modules.ERR_WRONG_VERSION)
	}
	if !strings.EqualFold(response.Type, modules.TYPE_CLIENT_RESPONSE){
		return fmt.Errorf(modules.ERR_TYPE_NOT_SUPPORTED)
	}
	return nil
}

func (s *OntLoginSdk) getRequiredVcTypes() []string {
	res := make([]string, 0)
	for _, vcf := range s.conf.vcfilters {
		if vcf.Required {
			res = append(res, vcf.Type)
		}
	}
	return res
}

func getDIDKeyAndIndex(verifymethod string) (string, int, error) {
	tmpArr := strings.Split(verifymethod, "#")
	if len(tmpArr) != 2 {
		return "", 0, fmt.Errorf("verificationMethod format invalid")
	}
	keyArr := strings.Split(tmpArr[1], "-")
	if len(keyArr) != 2 {
		return "", 0, fmt.Errorf("verificationMethod format invalid")
	}
	idx, err := strconv.Atoi(keyArr[1])
	return tmpArr[0], idx, err
}
