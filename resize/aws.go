package resize

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const instanceTypeURL = "http://aws.amazon.com/ec2/instance-types/"

type InstanceType struct {
	Name               string  // col 0
	CPUs               int     // col 1
	Memory             float64 // GiB col 2
	Storage            string  // GB col 3
	NetworkSpec        string  // col 4
	Processor          string  // col 5
	ClockSpeed         float64 // GHz col 6
	IntelAVX           bool    // col 7
	IntelAVX2          bool    // col 8
	IntelTurbo         bool    // col 9
	EBSOPT             bool    // col 10
	EnhancedNetworking bool    // col 11
}

// parseRow parses a row from the instance types matrix into it's given
// InstanceType
func parseRow(row *html.Node) (InstanceType, error) {
	cols := findAll(row, byTag(atom.Td))
	if len(cols) != 12 {
		return InstanceType{}, fmt.Errorf("expected 12 columns, got %d", len(cols))
	}
	yesNo := func(col *html.Node) bool {
		return strings.ToLower(text(col)) == "yes"
	}
	t := InstanceType{
		Name:               text(cols[0]),
		Storage:            text(cols[3]),
		NetworkSpec:        text(cols[4]),
		Processor:          text(cols[5]),
		IntelAVX:           yesNo(cols[7]),
		IntelAVX2:          yesNo(cols[8]),
		IntelTurbo:         yesNo(cols[9]),
		EBSOPT:             yesNo(cols[10]),
		EnhancedNetworking: yesNo(cols[11]),
	}
	var err error
	t.CPUs, err = strconv.Atoi(text(cols[1]))
	if err != nil {
		err = fmt.Errorf("expected number for CPUs, got '%s'", text(cols[1]))
		return InstanceType{}, err
	}
	t.Memory, err = strconv.ParseFloat(text(cols[2]), 64)
	if err != nil {
		err = fmt.Errorf("expected number for Memory, got '%s'", text(cols[2]))
		return InstanceType{}, err
	}

	t.ClockSpeed, err = strconv.ParseFloat(text(cols[6]), 64)
	if err != nil {
		err = fmt.Errorf("expected number for Memory, got '%s'", text(cols[2]))
		return InstanceType{}, err
	}
	return t, nil
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

type nodeMatcher func(*html.Node) bool

func findAll(n *html.Node, matcher nodeMatcher) []*html.Node {
	if matcher(n) {
		return []*html.Node{n}
	}
	matched := []*html.Node{}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		matched = append(matched, findAll(c, matcher)...)
	}
	return matched
}

func byTag(a atom.Atom) nodeMatcher {
	return func(n *html.Node) bool { return n.DataAtom == a }
}

func text(n *html.Node) string {
	textNodes := findAll(n, func(n *html.Node) bool { return n.Type == html.TextNode })
	parts := make([]string, len(textNodes))
	for i, node := range textNodes {
		parts[i] = node.Data
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

// InstanceTypes makes a request to AWS and parses the current available EC2
// instance types. Since this information is not available from the EC2 api,
// we must scrape it ourselves.
func InstanceTypes(client *http.Client) ([]InstanceType, error) {
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Get(instanceTypeURL)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bad response from AWS: %s", resp.Status)
	}

	root, err := html.Parse(resp.Body)
	if err != nil {
		return nil, err
	}

	var findMatrix func(node *html.Node) (*html.Node, bool)
	findMatrix = func(node *html.Node) (*html.Node, bool) {
		if attr(node, "id") == "instance-type-matrix" {
			return node, true
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			n, ok := findMatrix(c)
			if ok {
				return n, true
			}
		}
		return nil, false
	}
	matrixHeader, ok := findMatrix(root)
	if !ok {
		return nil, fmt.Errorf("no node with id 'instance-type-matrix'")
	}

	contains := func(sli []string, ele string) bool {
		for _, s := range sli {
			if s == ele {
				return true
			}
		}
		return false
	}

	var section *html.Node
	for section = matrixHeader.Parent; section != nil; section = section.Parent {
		classes := strings.Fields(attr(section, "class"))
		if contains(classes, "section") && contains(classes, "title-wrapper") {
			break
		}
	}
	if section == nil {
		return nil, fmt.Errorf("malformed HTML: title-wrapper not found")
	}
	var next *html.Node
	for next = section.NextSibling; next != nil; next = next.NextSibling {
		classes := strings.Fields(attr(next, "class"))
		if contains(classes, "section") && contains(classes, "table-wrapper") {
			break
		}
	}
	if next == nil {
		return nil, fmt.Errorf("malformed HTML: table-wrapper not found")
	}
	rows := findAll(next, byTag(atom.Tr))

	if len(rows) < 3 {
		return nil, fmt.Errorf("malformed HTML: could not find table")
	}
	rows = rows[1:]
	types := make([]InstanceType, len(rows))
	for i, row := range rows {
		types[i], err = parseRow(row)
		if err != nil {
			return nil, err
		}
	}
	return types, nil
}