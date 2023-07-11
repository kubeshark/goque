package goque

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"os"
	"sync"

	"github.com/syndtr/goleveldb/leveldb"
)

// Queue is a standard FIFO (first in, first out) queue.
type Queue struct {
	sync.RWMutex
	DataDir  string
	db       *leveldb.DB
	headInit uint64
	heads    []uint64
	tail     uint64
	isOpen   bool
}

// OpenQueue opens a queue if one exists at the given directory. If one
// does not already exist, a new queue is created.
func OpenQueue(dataDir string) (*Queue, error) {
	var err error

	// Create a new Queue.
	q := &Queue{
		DataDir:  dataDir,
		db:       &leveldb.DB{},
		headInit: 0,
		heads:    []uint64{},
		tail:     0,
		isOpen:   false,
	}

	// Open database for the queue.
	q.db, err = leveldb.OpenFile(dataDir, nil)
	if err != nil {
		return q, err
	}

	// Check if this Goque type can open the requested data directory.
	ok, err := checkGoqueType(dataDir, goqueQueue)
	if err != nil {
		return q, err
	}
	if !ok {
		return q, ErrIncompatibleType
	}

	// Set isOpen and return.
	q.isOpen = true
	return q, q.init()
}

// Enqueue adds an item to the queue.
func (q *Queue) Enqueue(value []byte) (*Item, error) {
	q.Lock()
	defer q.Unlock()

	// Check if queue is closed.
	if !q.isOpen {
		return nil, ErrDBClosed
	}

	// Create new Item.
	item := &Item{
		ID:    q.tail + 1,
		Key:   idToKey(q.tail + 1),
		Value: value,
	}

	// Add it to the queue.
	if err := q.db.Put(item.Key, item.Value, nil); err != nil {
		return nil, err
	}

	// Increment tail position.
	q.tail++

	return item, nil
}

// EnqueueString is a helper function for Enqueue that accepts a
// value as a string rather than a byte slice.
func (q *Queue) EnqueueString(value string) (*Item, error) {
	return q.Enqueue([]byte(value))
}

// EnqueueObject is a helper function for Enqueue that accepts any
// value type, which is then encoded into a byte slice using
// encoding/gob.
//
// Objects containing pointers with zero values will decode to nil
// when using this function. This is due to how the encoding/gob
// package works. Because of this, you should only use this function
// to encode simple types.
func (q *Queue) EnqueueObject(value interface{}) (*Item, error) {
	var buffer bytes.Buffer
	enc := gob.NewEncoder(&buffer)
	if err := enc.Encode(value); err != nil {
		return nil, err
	}

	return q.Enqueue(buffer.Bytes())
}

// EnqueueObjectAsJSON is a helper function for Enqueue that accepts
// any value type, which is then encoded into a JSON byte slice using
// encoding/json.
//
// Use this function to handle encoding of complex types.
func (q *Queue) EnqueueObjectAsJSON(value interface{}) (*Item, error) {
	jsonBytes, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}

	return q.Enqueue(jsonBytes)
}

// Dequeue removes the next item in the queue and returns it.
func (q *Queue) Dequeue(i int) (*Item, error) {
	q.Lock()
	defer q.Unlock()

	// Check if queue is closed.
	if !q.isOpen {
		return nil, ErrDBClosed
	}

	if len(q.heads) < i+1 {
		q.heads = append(q.heads, q.headInit)
	}

	// Try to get the next item in the queue.
	item, err := q.getItemByID(q.heads[i] + 1)
	if err != nil {
		return nil, err
	}

	// Increment head position.
	q.heads[i]++

	return item, nil
}

// Close closes the LevelDB database of the queue.
func (q *Queue) Close() error {
	q.Lock()
	defer q.Unlock()

	// Check if queue is already closed.
	if !q.isOpen {
		return nil
	}

	// Close the LevelDB database.
	if err := q.db.Close(); err != nil {
		return err
	}

	// Reset queue head and tail and set
	// isOpen to false.
	q.headInit = 0
	q.heads = []uint64{}
	q.tail = 0
	q.isOpen = false

	return nil
}

// Drop closes and deletes the LevelDB database of the queue.
func (q *Queue) Drop() error {
	if err := q.Close(); err != nil {
		return err
	}

	return os.RemoveAll(q.DataDir)
}

// getItemByID returns an item, if found, for the given ID.
func (q *Queue) getItemByID(id uint64) (*Item, error) {
	// Get item from database.
	var err error
	item := &Item{ID: id, Key: idToKey(id)}
	if item.Value, err = q.db.Get(item.Key, nil); err != nil {
		return nil, err
	}

	return item, nil
}

// init initializes the queue data.
func (q *Queue) init() error {
	// Create a new LevelDB Iterator.
	iter := q.db.NewIterator(nil, nil)
	defer iter.Release()

	// Set queue head to the first item.
	if iter.First() {
		q.headInit = keyToID(iter.Key()) - 1
	}

	// Set queue tail to the last item.
	if iter.Last() {
		q.tail = keyToID(iter.Key())
	}

	return iter.Error()
}
