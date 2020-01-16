package driver

import (
	"hash/fnv"
	"strconv"
)

func genDiskID(str string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(str))
	return "csi-" + strconv.Itoa(int(h.Sum32()))
}
