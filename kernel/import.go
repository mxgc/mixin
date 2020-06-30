package kernel

import (
	"fmt"
	"time"

	"github.com/MixinNetwork/mixin/common"
	"github.com/MixinNetwork/mixin/logger"
	"github.com/MixinNetwork/mixin/storage"
)

func (node *Node) Import(configDir string, store, source storage.Store) error {
	_, gss, _, err := node.buildGenesisSnapshots(configDir)
	if err != nil {
		return err
	}
	kss, err := store.ReadSnapshotsSinceTopology(0, 100)
	if err != nil {
		return err
	}
	if len(gss) != len(kss) {
		return fmt.Errorf("kernel already initilaized %d %d", len(gss), len(kss))
	}

	for i, gs := range gss {
		ks := kss[i]
		if ks.PayloadHash() != gs.PayloadHash() {
			return fmt.Errorf("kernel genesis unmatch %d %s %s", i, gs.PayloadHash(), ks.PayloadHash())
		}
	}

	go node.CosiLoop()

	var latestSnapshots []*common.SnapshotWithTopologicalOrder
	offset, limit := uint64(0), uint64(200)
	for {
		snapshots, transactions, err := source.ReadSnapshotWithTransactionsSinceTopology(offset, limit)
		if err != nil {
			logger.Printf("source.ReadSnapshotWithTransactionsSinceTopology(%d, %d) %s\n", offset, limit, err)
		}

		for i, s := range snapshots {
			tx := transactions[i]
			if s.Transaction != tx.PayloadHash() {
				return fmt.Errorf("malformed transaction hash %s %s", s.Transaction, tx.PayloadHash())
			}
			old, _, err := store.ReadTransaction(s.Transaction)
			if err != nil {
				return fmt.Errorf("ReadTransaction %s %s", s.Transaction, err)
			}

			if old == nil {
				err = node.validateKernelSnapshot(&s.Snapshot, tx, true)
				if err != nil {
					return fmt.Errorf("validateKernelSnapshot %s %s", s.Transaction, err)
				}

				err = tx.LockInputs(node.persistStore, true)
				if err != nil {
					return fmt.Errorf("LockInputs %s %s", s.Transaction, err)
				}
				err := store.WriteTransaction(tx)
				if err != nil {
					return fmt.Errorf("WriteTransaction %s %s", s.Transaction, err)
				}
			}

			err = node.QueueAppendSnapshot(node.IdForNetwork, &s.Snapshot, true)
			if err != nil {
				return fmt.Errorf("QueueAppendSnapshot %s %s", s.Transaction, err)
			}
		}

		if len(snapshots) > 0 {
			offset += limit
			latestSnapshots = snapshots
		}

		if uint64(len(snapshots)) != limit {
			logger.Printf("source.ReadSnapshotWithTransactionsSinceTopology(%d, %d) DONE %d\n", offset, limit, len(snapshots))
			break
		}
	}

	for {
		time.Sleep(1 * time.Minute)
		fc, _, err := store.QueueInfo()
		if err != nil || fc > 0 {
			logger.Printf("store.QueueInfo() %d, %s\n", fc, err)
			continue
		}
		var pending bool
		for _, s := range latestSnapshots {
			ss, err := store.ReadSnapshot(s.Hash)
			if err != nil || ss == nil {
				logger.Printf("store.ReadSnapshot(%s) %v, %s\n", s.Hash, ss, err)
				pending = true
				break
			}
		}
		if !pending {
			break
		}
	}

	return nil
}