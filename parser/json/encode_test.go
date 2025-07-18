package jsonparser

import (
	"bytes"
	"testing"

	"github.com/anfin21/socket.io/parser"
	"github.com/anfin21/socket.io/parser/json/serializer/stdjson"
	"github.com/cristalhq/jsn"
)

func TestEncode(t *testing.T) {
	c := NewCreator(0, stdjson.New())
	p := c()
	tests := createEncodeTests(t)

	for i, test := range tests {
		t.Logf("\nTEST %d\n", i)

		buffers, err := p.Encode(test.Header, test.V)
		if err != nil {
			t.Fatal(err)
		}

		for i, buf := range buffers {
			t.Logf("buf #%d: %s", i, buf)
		}

		if len(buffers) > 1 && !test.Header.IsBinary() {
			t.Fatal("type of the packet should have been binary")
		}

		if test.Header.IsEvent() && len(buffers[0]) == 0 {
			t.Fatal("payload of an EVENT packet cannot be empty")
		}

		if len(test.Expected) != len(buffers) {
			t.Fatal("length of the test buffers and encoded buffers didn't match")
		}

		for i, buf := range buffers {
			expected1 := test.Expected[i]
			if !bytes.Equal(buf, expected1) {
				if len(test.Or) > 0 {
					expected2 := test.Or[i]
					if !bytes.Equal(buf, expected2) {
						t.Fatalf("test buffers and encoded buffer didn't match (buffer #%d)\nbuf: `%s`\n", i, buf)
					}
				} else {
					t.Fatalf("test buffer and encoded buffer didn't match (buffer #%d)\nbuf: `%s`\n", i, buf)
				}
			}
		}
	}
}

func TestMaxAttachmentsEncode(t *testing.T) {
	c := NewCreator(3, stdjson.New())
	p := c()

	header := &parser.PacketHeader{
		Type: parser.PacketTypeEvent,
	}

	v := struct {
		A1 Binary `json:"a1"`
		A2 Binary `json:"a2"`
		A3 Binary `json:"a3"`
		A4 Binary `json:"a4"`
	}{
		A1: Binary("a1"),
		A2: Binary("a2"),
		A3: Binary("a3"),
		A4: Binary("a4"),
	}

	_, err := p.Encode(header, &v)
	if err == nil {
		t.Fatal("error expected")
	}
}

func createEncodeTests(t *testing.T) []*encodeTest {
	type person struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
		Bin  Binary `json:"bin,omitempty"`
		V    any    `json:"v,omitempty"`
	}

	return []*encodeTest{
		{
			Header:   mustCreatePacketHeader(t, parser.PacketTypeConnect, "", 0),
			Expected: createBuffers([]byte("0")),
		},
		{
			Header:   mustCreatePacketHeader(t, parser.PacketTypeConnect, "/", 0),
			Expected: createBuffers([]byte("0")),
		},
		{
			Header:   mustCreatePacketHeader(t, parser.PacketTypeDisconnect, "/", 0),
			Expected: createBuffers([]byte("1")),
		},
		{
			Header:   mustCreatePacketHeader(t, parser.PacketTypeEvent, "/", 0),
			Expected: createBuffers([]byte("2")),
		},
		{
			Header:   mustCreatePacketHeader(t, parser.PacketTypeAck, "/", 0),
			Expected: createBuffers([]byte("3")),
		},
		{
			Header:   mustCreatePacketHeader(t, parser.PacketTypeConnectError, "/", 0),
			Expected: createBuffers([]byte("4")),
		},
		{
			Header:   mustCreatePacketHeader(t, parser.PacketTypeConnect, "/", 12345678912345678912),
			Expected: createBuffers([]byte("012345678912345678912")),
		},
		{
			Header:   mustCreatePacketHeader(t, parser.PacketTypeConnect, "/Wolfeschlegelsteinhausenbergerdorffwelchevoralternwarengewissenhaftschaferswessenschafewarenwohlgepflegeundsorgfaltigkeitbeschutzenvonangreifendurchihrraubgierigfeindewelchevoralternzwolftausendjahresvorandieerscheinenvanderersteerdemenschderraumschiffgebrauchlichtalsseinursprungvonkraftgestartseinlangefahrthinzwischensternartigraumaufdersuchenachdiesternwelchegehabtbewohnbarplanetenkreisedrehensichundwohinderneurassevonverstandigmenschlichkeitkonntefortpflanzenundsicherfreuenanlebenslanglichfreudeundruhemitnichteinfurchtvorangreifenvonandererintelligentgeschopfsvonhinzwischensternartigraum", 0),
			Expected: createBuffers([]byte("0/Wolfeschlegelsteinhausenbergerdorffwelchevoralternwarengewissenhaftschaferswessenschafewarenwohlgepflegeundsorgfaltigkeitbeschutzenvonangreifendurchihrraubgierigfeindewelchevoralternzwolftausendjahresvorandieerscheinenvanderersteerdemenschderraumschiffgebrauchlichtalsseinursprungvonkraftgestartseinlangefahrthinzwischensternartigraumaufdersuchenachdiesternwelchegehabtbewohnbarplanetenkreisedrehensichundwohinderneurassevonverstandigmenschlichkeitkonntefortpflanzenundsicherfreuenanlebenslanglichfreudeundruhemitnichteinfurchtvorangreifenvonandererintelligentgeschopfsvonhinzwischensternartigraum,")),
		},
		{
			Header:   mustCreatePacketHeader(t, parser.PacketTypeConnect, "/Wolfeschlegelsteinhausenbergerdorffwelchevoralternwarengewissenhaftschaferswessenschafewarenwohlgepflegeundsorgfaltigkeitbeschutzenvonangreifendurchihrraubgierigfeindewelchevoralternzwolftausendjahresvorandieerscheinenvanderersteerdemenschderraumschiffgebrauchlichtalsseinursprungvonkraftgestartseinlangefahrthinzwischensternartigraumaufdersuchenachdiesternwelchegehabtbewohnbarplanetenkreisedrehensichundwohinderneurassevonverstandigmenschlichkeitkonntefortpflanzenundsicherfreuenanlebenslanglichfreudeundruhemitnichteinfurchtvorangreifenvonandererintelligentgeschopfsvonhinzwischensternartigraum", 12345678912345678912),
			Expected: createBuffers([]byte("0/Wolfeschlegelsteinhausenbergerdorffwelchevoralternwarengewissenhaftschaferswessenschafewarenwohlgepflegeundsorgfaltigkeitbeschutzenvonangreifendurchihrraubgierigfeindewelchevoralternzwolftausendjahresvorandieerscheinenvanderersteerdemenschderraumschiffgebrauchlichtalsseinursprungvonkraftgestartseinlangefahrthinzwischensternartigraumaufdersuchenachdiesternwelchegehabtbewohnbarplanetenkreisedrehensichundwohinderneurassevonverstandigmenschlichkeitkonntefortpflanzenundsicherfreuenanlebenslanglichfreudeundruhemitnichteinfurchtvorangreifenvonandererintelligentgeschopfsvonhinzwischensternartigraum,12345678912345678912")),
		},
		{
			Header:   mustCreatePacketHeader(t, parser.PacketTypeConnect, "/", 0),
			V:        nil,
			Expected: createBuffers([]byte("0")),
		},
		{
			Header: mustCreatePacketHeader(t, parser.PacketTypeConnect, "/", 0),
			V: &person{
				Name: "Abdurrezak",
				Age:  25,
			},
			Expected: createBuffers([]byte(`0{"name":"Abdurrezak","age":25}`)),
		},
		{
			Header: mustCreatePacketHeader(t, parser.PacketTypeConnect, "/", 0),
			V: person{
				Name: "Abdurrezak",
				Age:  25,
			},
			Expected: createBuffers([]byte(`0{"name":"Abdurrezak","age":25}`)),
		},
		{
			Header:   mustCreatePacketHeader(t, parser.PacketTypeEvent, "/", 0),
			V:        createArgs("EVENT_NAME", 1, 2, 3, "1", "2", "3"),
			Expected: createBuffers([]byte(`2["EVENT_NAME",1,2,3,"1","2","3"]`)),
		},
		{
			Header:   mustCreatePacketHeader(t, parser.PacketTypeEvent, "/", 0),
			V:        createArgs("EVENT_NAME"),
			Expected: createBuffers([]byte(`2["EVENT_NAME"]`)),
		},
		{
			Header:   mustCreatePacketHeader(t, parser.PacketTypeEvent, "/", 0),
			V:        createArgs("EVENT_NAME", Binary("\x00\x01\x02\x05\x06\x07\x08\x09")),
			Expected: createBuffers([]byte(`51-["EVENT_NAME",{"_placeholder":true,"num":0}]`), []byte("\x00\x01\x02\x05\x06\x07\x08\x09")),
		},
		{
			Header: mustCreatePacketHeader(t, parser.PacketTypeEvent, "/", 0),
			V: createArgs(
				"EVENT_NAME",
				&person{
					Name: "Abdurrezak",
					Age:  25,
					Bin:  Binary("This is binary"),
				},
			),
			Expected: createBuffers([]byte(`51-["EVENT_NAME",{"name":"Abdurrezak","age":25,"bin":{"_placeholder":true,"num":0}}]`), []byte("This is binary")),
		},
		{
			Header: mustCreatePacketHeader(t, parser.PacketTypeAck, "/", 0),
			V: createArgs(
				jsn.O{ // This is map[string]any
					"lorem": "ipsum",
					"dolor": 12345,
					"amet":  Binary("This is binary"),
				},
			),
			Expected: createBuffers([]byte(`61-[{"amet":{"_placeholder":true,"num":0},"dolor":12345,"lorem":"ipsum"}]`), []byte("This is binary")),
		},
		{
			Header: mustCreatePacketHeader(t, parser.PacketTypeAck, "/", 0),
			V: createArgs(
				jsn.O{ // This is map[string]any
					"lorem": "ipsum",
					"dolor": 12345,
					"amet":  Binary("One"),
					"consectetur": &person{
						Name: "Abdurrezak",
						Age:  25,
						Bin:  Binary("Two"),
					},
				},
			),
			Expected: createBuffers([]byte(`62-[{"amet":{"_placeholder":true,"num":0},"consectetur":{"name":"Abdurrezak","age":25,"bin":{"_placeholder":true,"num":1}},"dolor":12345,"lorem":"ipsum"}]`), []byte("One"), []byte("Two")),
			Or:       createBuffers([]byte(`62-[{"amet":{"_placeholder":true,"num":1},"consectetur":{"name":"Abdurrezak","age":25,"bin":{"_placeholder":true,"num":0}},"dolor":12345,"lorem":"ipsum"}]`), []byte("Two"), []byte("One")),
		},
		{
			Header: mustCreatePacketHeader(t, parser.PacketTypeAck, "/", 0),
			V: createArgs(
				&person{
					Name: "Abdurrezak",
					Age:  25,
					Bin:  Binary("One"),
					V: jsn.O{ // This is map[string]any
						"lorem": "ipsum",
						"dolor": 12345,
						"amet":  Binary("Two"),
					},
				},
			),
			Expected: createBuffers([]byte(`62-[{"name":"Abdurrezak","age":25,"bin":{"_placeholder":true,"num":0},"v":{"amet":{"_placeholder":true,"num":1},"dolor":12345,"lorem":"ipsum"}}]`), []byte("One"), []byte("Two")),
			Or:       createBuffers([]byte(`62-[{"name":"Abdurrezak","age":25,"bin":{"_placeholder":true,"num":1},"v":{"amet":{"_placeholder":true,"num":0},"dolor":12345,"lorem":"ipsum"}}]`), []byte("Two"), []byte("One")),
		},
	}
}

func createArgs(v ...any) *[]any {
	return &v
}

type encodeTest struct {
	Header   *parser.PacketHeader
	V        any
	Expected [][]byte
	Or       [][]byte
}

func mustCreatePacketHeader(_ *testing.T, packetType parser.PacketType, namespace string, ackID uint64) *parser.PacketHeader {
	ackIDPtr := &ackID
	if ackID == 0 {
		ackIDPtr = nil
	}

	return &parser.PacketHeader{
		Type:      packetType,
		Namespace: namespace,
		ID:        ackIDPtr,
	}
}
