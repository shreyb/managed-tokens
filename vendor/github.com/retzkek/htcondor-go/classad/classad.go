package classad

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// AttributeType represents the supported Classad attribute types
type AttributeType int

// ClassAd attribute types
const (
	Integer AttributeType = iota
	Real
	String
	Undefined
	Error
)

// Attribute represents a typed Classad attribute.
type Attribute struct {
	Type  AttributeType
	Value interface{}
}

func AttributeFromString(val string) Attribute {
	val = strings.Trim(val, " ")
	if len(val) == 0 {
		return Attribute{Type: Error}
	}
	if val[0] != '"' {
		// not a string, see if it's an integer
		ival, err := strconv.ParseInt(val, 10, 64)
		if err == nil {
			return Attribute{Type: Integer, Value: ival}
		}
		// how about a real
		fval, err := strconv.ParseFloat(val, 64)
		if err == nil {
			return Attribute{Type: Real, Value: fval}
		}
	}
	return Attribute{
		Type:  String,
		Value: strings.Trim(val, "\""),
	}
}

// String returns the string representation of the ClassAd attribute.
func (a Attribute) String() string {
	switch a.Type {
	case Integer:
		return fmt.Sprintf("%d", a.Value)
	case Real:
		return fmt.Sprintf("%f", a.Value)
	case String:
		return fmt.Sprintf("%s", a.Value)
	case Undefined:
		return "UNDEFINED"
	case Error:
		return "ERROR"
	}
	return "TYPEERROR"
}

// ClassAd represents an HTCondor ClassAd (see http://research.cs.wisc.edu/htcondor/manual/current/4_1HTCondor_s_ClassAd.html).
type ClassAd map[string]Attribute

// ReadClassAds reads multiple ClassAds (in "long" format) from r until EOF.
// ClassAds should be separated by a blank line.
// Numeric attributes are returned as such, but expressions are not evaluated and are returned as strings.
func ReadClassAds(r io.Reader) ([]ClassAd, error) {
	scanner := bufio.NewScanner(r)
	ads := make([]ClassAd, 0)
	ad := make(ClassAd)
	for scanner.Scan() {
		if scanner.Text() == "" {
			if len(ad) > 0 {
				ads = append(ads, ad)
				ad = make(ClassAd)
			}
			continue
		}
		// Naïve tokenizing and parsing of long format.
		parts := strings.SplitN(scanner.Text(), "=", 2)
		if len(parts) < 2 {
			return nil, fmt.Errorf("invalid classad attribute: \"%s\"", scanner.Text())
		}
		key := strings.Trim(parts[0], " \"")
		ad[key] = AttributeFromString(parts[1])
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(ad) > 0 {
		ads = append(ads, ad)
	}
	return ads, nil
}

// StreamClassAds reads multiple ClassAds (in "long" format) from r
// until EOF, writing them to the supplied channel, which is closed
// when all are read or upon error.  ClassAds should be separated by a
// blank line.  Numeric attributes are returned as such, but
// expressions are not evaluated and are returned as strings.  If
// errors are encountered reading the classads, they will be sent on
// the errors channel.
func StreamClassAds(r io.Reader, ch chan ClassAd, errors chan error) {
	defer close(ch)
	defer close(errors)
	scanner := bufio.NewScanner(r)
	ad := make(ClassAd)
	for scanner.Scan() {
		if scanner.Text() == "" {
			if len(ad) > 0 {
				ch <- ad
				ad = make(ClassAd)
			}
			continue
		}
		// Naïve tokenizing and parsing of long format.
		parts := strings.SplitN(scanner.Text(), "=", 2)
		if len(parts) < 2 {
			errors <- fmt.Errorf("invalid classad attribute: \"%s\"", scanner.Text())
			continue
		}
		key := strings.Trim(parts[0], " \"")
		ad[key] = AttributeFromString(parts[1])
	}
	if err := scanner.Err(); err != nil {
		errors <- fmt.Errorf("scanner error: %s", err)
	}
	if len(ad) > 0 {
		ch <- ad
	}
}

// Strings returns a map of the string representation for all the attributes in the ClassAd.
func (c ClassAd) Strings() map[string]string {
	ad := make(map[string]string, len(c))
	for k, v := range c {
		ad[k] = v.String()
	}
	return ad
}

// MarshalJSON returns the ClassAd as a JSON document.
func (c ClassAd) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	fmt.Fprintf(&b, "{")
	first := true
	for k, v := range c {
		if first {
			first = false
		} else {
			fmt.Fprintf(&b, ",")
		}
		fmt.Fprintf(&b, "\"%s\":", k)
		switch v.Type {
		case Integer, Real:
			fmt.Fprintf(&b, "%s", v.String())
		default:
			fmt.Fprintf(&b, "\"%s\"", v.String())
		}
	}
	fmt.Fprintf(&b, "}")
	return b.Bytes(), nil
}
