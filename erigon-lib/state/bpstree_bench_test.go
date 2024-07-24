package state

import (
	"github.com/stretchr/testify/require"
	"path"
	"path/filepath"
	"testing"

	"github.com/erigontech/erigon-lib/log/v3"
)

func BenchmarkBpsTreeSeek(t *testing.B) {
	tmp := t.TempDir()
	logger := log.New()
	keyCount, M := 12000000, 256
	compressFlags := CompressKeys | CompressVals

	dataPath := generateKV(t, tmp, 52, 180, keyCount, logger, 0)

	indexPath := path.Join(tmp, filepath.Base(dataPath)+".bti")
	buildBtreeIndex(t, dataPath, indexPath, compressFlags, 1, logger, true)

	kv, bt, err := OpenBtreeIndexAndDataFile(indexPath, dataPath, uint64(M), compressFlags, false)
	require.NoError(t, err)
	require.EqualValues(t, bt.KeyCount(), keyCount)
	defer bt.Close()
	defer kv.Close()

	var key []byte

	getter := NewArchiveGetter(kv.MakeGetter(), compressFlags)
	getter.Reset(0)

	t.ResetTimer()
	t.ReportAllocs()
	//r := rand.New(rand.NewSource(0))
	for i := 0; i < t.N; i++ {
		if !getter.HasNext() {
			getter.Reset(0)
		}
		key, _ = getter.Next(key[:0])
		getter.Skip()
		//_, err := bt.Seek(getter, keys[r.Intn(len(keys))])
		_, err := bt.Seek(getter, key)
		require.NoError(t, err)
	}
	t.ReportAllocs()
}
