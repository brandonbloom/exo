package compose

import (
	"errors"
	"regexp"
	"strings"
)

type PortMappings []PortMapping

type PortMapping struct {
	Target    string `yaml:"target"`
	Published string `yaml:"published"`
	HostIP    string `yaml:"host_ip"`
	Protocol  string `yaml:"protocol"`
	Mode      string `yaml:"mode"`
}

func ParsePortMappings(short string) (mappings PortMappings, err error) {
	parts := strings.Split(short, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		mapping, err := ParsePortMapping(part)
		if err != nil {
			return nil, err
		}
		mappings = append(mappings, mapping)
	}
	return mappings, nil
}

func ParsePortMapping(short string) (PortMapping, error) {
	submatches := portRegexp.FindStringSubmatch(short)
	if len(submatches) == 0 {
		return PortMapping{}, errors.New("invalid port mapping syntax")
	}
	var mapping PortMapping
	mapping.HostIP = submatches[2]
	mapping.Published = submatches[4]
	mapping.Target = submatches[5]
	mapping.Protocol = submatches[7]
	return mapping, nil
}

var portRegexp = regexp.MustCompile("^((\\d+\\.\\d+\\.\\d+\\.\\d+):)?(([-\\d]+):)?([-\\d]+)(/(.+))?$")

func (mappings PortMappings) MarshalYAML() (interface{}, error) {
	res := make([]interface{}, len(mappings))
	for i, x := range mappings {
		res[i] = x // TODO: Marshal to short syntax if possible.
	}
	return res, nil
}

func (mappings *PortMappings) UnmarshalYAML(unmarshal func(interface{}) error) error {
	*mappings = []PortMapping{}
	var s string
	err := unmarshal(&s)
	if err == nil {
		*mappings, err = ParsePortMappings(s)
	} else {
		var xs []portMappingMarshaller
		err = unmarshal(&xs)
		for _, x := range xs {
			*mappings = append(*mappings, PortMapping(x))
		}
	}
	return err
}

type portMappingMarshaller PortMapping

func (marshaller *portMappingMarshaller) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	var mapping PortMapping
	err := unmarshal(&s)
	if err == nil {
		mapping, err = ParsePortMapping(s)
	} else {
		err = unmarshal(&mapping)
	}
	*marshaller = portMappingMarshaller(mapping)
	return err
}