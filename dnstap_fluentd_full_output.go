/*
 * Copyright (c) 2018 Manabu Sonoda
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package dtap

import (
	"context"
	"net"
	"strings"
	"time"

	"github.com/pkg/errors"

	"github.com/fluent/fluent-logger-golang/fluent"

	log "github.com/sirupsen/logrus"

	dnstap "github.com/dnstap/golang-dnstap"
	"github.com/miekg/dns"
)

type DnstapFluentFullOutput struct {
	config   *OutputFluentConfig
	ipv4Mask net.IPMask
	ipv6Mask net.IPMask
	tag      string
}

var names = map[int]string{
	2: "tld",
	3: "2ld",
	4: "3ld",
	5: "4ld",
}

func NewDnstapFluentFullOutput(config *OutputFluentConfig) (*DnstapFluentdOutput, error) {
	mo := &DnstapFluentFullOutput{
		config:   config,
		ipv4Mask: net.CIDRMask(config.GetIPv4Mask(), 32),
		ipv6Mask: net.CIDRMask(config.GetIPv6Mask(), 128),
		tag:      config.GetTag(),
	}
	o, err := NewDnstapFluentdOutput(config, mo)
	if err != nil {
		return nil, errors.Wrapf(err, "can't create DnstapFluentdOutput")
	}
	return o, nil
}
func (o *DnstapFluentFullOutput) setup(ctx context.Context) {

}
func (o *DnstapFluentFullOutput) handle(client *fluent.Fluent, dt *dnstap.Dnstap, errCh chan error) {
	var dnsMessage []byte
	msg := dt.GetMessage()
	dnsMsg := dns.Msg{}
	if msg.GetQueryMessage != nil {
		dnsMessage = msg.GetQueryMessage()
	} else {
		dnsMessage = msg.GetResponseMessage()
	}
	if err := dnsMsg.Unpack(dnsMessage); err != nil {
		if log.GetLevel() >= log.DebugLevel {
			errCh <- errors.Wrapf(err, "can't parse dns message() failed: %s\n", err)
		}
		return
	}

	var data = map[string]interface{}{}
	switch msg.GetType() {
	case dnstap.Message_AUTH_QUERY, dnstap.Message_RESOLVER_QUERY,
		dnstap.Message_CLIENT_QUERY, dnstap.Message_FORWARDER_QUERY,
		dnstap.Message_STUB_QUERY, dnstap.Message_TOOL_QUERY:
		data["@timestamp"] = time.Unix(int64(msg.GetQueryTimeSec()), int64(msg.GetQueryTimeNsec())).Format(time.RFC3339Nano)
		data["identity"] = dt.GetIdentity()
		if len(msg.GetQueryAddress()) == 4 {
			data["query_address"] = net.IP(msg.GetQueryAddress()).Mask(o.ipv4Mask).String()
		} else {
			data["query_address"] = net.IP(msg.GetQueryAddress()).Mask(o.ipv6Mask).String()
		}
		data["query_port"] = msg.GetQueryPort()
	case dnstap.Message_AUTH_RESPONSE, dnstap.Message_RESOLVER_RESPONSE,
		dnstap.Message_CLIENT_RESPONSE, dnstap.Message_FORWARDER_RESPONSE,
		dnstap.Message_STUB_RESPONSE, dnstap.Message_TOOL_RESPONSE:
		data["@timestamp"] = time.Unix(int64(msg.GetResponseTimeSec()), int64(msg.GetResponseTimeNsec())).Format(time.RFC3339Nano)
		data["identity"] = dt.GetIdentity()
		if len(msg.GetResponseAddress()) == 4 {
			data["response_address"] = net.IP(msg.GetResponseAddress()).Mask(o.ipv4Mask).String()
		} else {
			data["response_address"] = net.IP(msg.GetResponseAddress()).Mask(o.ipv6Mask).String()
		}
		data["response_port"] = msg.GetResponsePort()
		data["response_zone"] = msg.GetQueryZone()
	}
	data["type"] = msg.GetType().String()
	data["socket_family"] = msg.GetSocketFamily().String()
	data["socket_protocol"] = msg.GetSocketProtocol().String()
	data["version"] = dt.GetVersion()
	data["extra"] = dt.GetExtra()
	data["qname"] = dnsMsg.Question[0].Name
	data["qclass"] = dns.ClassToString[dnsMsg.Question[0].Qclass]
	data["qtype"] = dns.TypeToString[dnsMsg.Question[0].Qtype]
	data["rcode"] = dns.RcodeToString[dnsMsg.Rcode]
	data["aa"] = dnsMsg.Authoritative
	data["tc"] = dnsMsg.Truncated
	data["rd"] = dnsMsg.RecursionDesired
	data["ra"] = dnsMsg.RecursionAvailable
	data["ad"] = dnsMsg.AuthenticatedData
	data["cd"] = dnsMsg.CheckingDisabled

	labels := strings.Split(dnsMsg.Question[0].Name, ".")
	labelsLen := len(labels)
	for i, n := range names {
		if labelsLen-i >= 0 {
			data[n] = strings.Join(labels[labelsLen-i:labelsLen-1], ".")
		} else {
			data[n] = dnsMsg.Question[0].Name
		}
	}
	if err := client.Post(o.tag, data); err != nil {
		if log.GetLevel() >= log.WarnLevel {
			errCh <- errors.Wrapf(err, "failed to post fluent message, tag: %s", o.tag)
		}
	}
}
