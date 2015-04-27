package renter

import (
	"errors"

	"github.com/NebulousLabs/Sia/crypto"
	"github.com/NebulousLabs/Sia/modules"
	"github.com/NebulousLabs/Sia/types"
)

// A file is a single file that has been uploaded to the network.
type file struct {
	Name          string
	EncryptionKey crypto.TwofishKey
	Checksum      crypto.Hash // checksum of the decoded file.

	// Erasure coding variables:
	//		piecesRequired <= optimalRecoveryPieces <= totalPieces
	ErasureScheme         string
	PiecesRequired        int
	OptimalRecoveryPieces int
	TotalPieces           int
	Pieces                []filePiece

	// DEPRECATED - the new renter scheme has the renter pre-making contracts
	// with hosts uploading new contracts through diffs.
	UploadParams modules.FileUploadParams

	// The file needs to access the renter's lock. This variable is not
	// exported so that the persistence functions won't save the whole renter.
	renter *Renter
}

// A filePiece contains information about an individual file piece that has
// been uploaded to a host, including information about the host and the health
// of the file piece.
type filePiece struct {
	Active     bool                 // True if the host has the file and has been online somewhat recently.
	Repairing  bool                 // True if the piece is currently being uploaded.
	Contract   types.FileContract   // The contract being enforced.
	ContractID types.FileContractID // The ID of the contract.

	HostIP     modules.NetAddress // Where to find the file.
	StartIndex uint64
	EndIndex   uint64

	PieceIndex int // Indicates the erasure coding index of this piece.
	Checksum   crypto.Hash
}

// Available indicates whether the file is ready to be downloaded.
func (f *file) Available() bool {
	lockID := f.renter.mu.RLock()
	defer f.renter.mu.RUnlock(lockID)

	var active int
	for _, piece := range f.Pieces {
		if piece.Active {
			active++
		}
		if active >= f.PiecesRequired {
			return true
		}
	}
	return false
}

// Nickname returns the nickname of the file.
func (f *file) Nickname() string {
	lockID := f.renter.mu.RLock()
	defer f.renter.mu.RUnlock(lockID)
	return f.Name
}

// Repairing returns whether or not the file is actively being repaired.
func (f *file) Repairing() bool {
	lockID := f.renter.mu.RLock()
	defer f.renter.mu.RUnlock(lockID)

	for _, piece := range f.Pieces {
		if piece.Repairing {
			return true
		}
	}
	return false
}

// TimeRemaining returns the amount of time until the file's contracts expire.
func (f *file) TimeRemaining() types.BlockHeight {
	lockID := f.renter.mu.RLock()
	defer f.renter.mu.RUnlock(lockID)

	if len(f.Pieces) == 0 {
		return 0
	}
	if f.Pieces[0].Contract.WindowStart < f.renter.blockHeight {
		return 0
	}
	return f.Pieces[0].Contract.WindowStart - f.renter.blockHeight
}

// FileList returns all of the files that the renter has.
func (r *Renter) FileList() (files []modules.FileInfo) {
	lockID := r.mu.RLock()
	defer r.mu.RUnlock(lockID)

	for i := range r.files {
		// Because 'file' is the same memory for all iterations, we need to
		// make a copy.
		nf := new(file)
		*nf = r.files[i]
		files = append(files, nf)
	}
	return
}

// Rename takes an existing file and changes the nickname. The original file
// must exist, and there must not be any file that already has the replacement
// nickname.
func (r *Renter) Rename(currentName, newName string) error {
	lockID := r.mu.Lock()
	defer r.mu.Unlock(lockID)

	// Check that the currentName exists and the newName doesn't.
	entry, exists := r.files[currentName]
	if !exists {
		return errors.New("no file found by that name")
	}
	_, exists = r.files[newName]
	if exists {
		return errors.New("file of new name already exists")
	}

	// Do the renaming.
	delete(r.files, currentName)
	entry.Name = newName
	r.files[newName] = entry

	r.save()
	return nil
}
