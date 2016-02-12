//
// Resembles the FS directory structure, rather than the flat meta
//

package dirTree

import (
	"path"
	"strings"
	"syscall"

	log "github.com/Sirupsen/logrus"
	"github.com/muthu-r/horcrux"
)

const MIN_KIDS = 100

type Node struct {
	Entry horcrux.Entry

	numKids int
	kidsMap map[string]*Node
	kidsArr []string
}

func enqueue(q []*Node, node *Node) []*Node {
	q = append(q, node)
	/*
		log.WithFields(log.Fields{"nodeName": node.Entry.Name,
					"nodePrefix": node.Entry.Prefix,
				}).Debug("ENqueue")
	*/
	return q
}

func dequeue(q []*Node) ([]*Node, *Node) {
	if len(q) == 0 {
		return q, nil
	}

	n := q[0]
	q = q[1:]
	/*
		log.WithFields(log.Fields{"nodeName": n.Entry.Name,
					"nodePrefix": n.Entry.Prefix,
				}).Debug("DEqueue")
	*/
	return q, n
}

// Walks the tree and gets the flat Meta data
func GetMeta(root *Node) (*horcrux.Meta, error) {
	var Meta *horcrux.Meta
	var queue [MIN_KIDS * MIN_KIDS]*Node

	if root == nil {
		log.Error("GetMeta: nil root")
		return nil, syscall.EINVAL
	}

	q := queue[0:0]

	Meta = new(horcrux.Meta)
	Meta.Entries = make([]horcrux.Entry, 0, MIN_KIDS*MIN_KIDS)
	node := root
	idx := 0
	for node != nil {
		/*
		log.WithFields(log.Fields{
			"Idx":    idx,
			"Name":   node.Entry.Name,
			"Prefix": node.Entry.Prefix,
		}).Debug("Getting Entry")
		*/

		Meta.Entries = append(Meta.Entries, node.Entry)
		idx++
		for i := 0; i < node.numKids; i++ {
			name := node.kidsArr[i]
			n := node.kidsMap[name]
			/*
			log.WithFields(log.Fields{
				"Entry Name":   n.Entry.Name,
				"Entry Prefix": n.Entry.Prefix,
			}).Debug("Queuing node..")
			*/
			q = enqueue(q, n)
		}
		q, node = dequeue(q)
	}

	Meta.NumFiles = idx
	Meta.Entries = Meta.Entries[:idx]
	return Meta, nil
}

// Returns i-th kid for the node
func GetKid(node *Node, idx int) (Node, error) {
	if idx >= len(node.kidsArr) {
		return Node{}, syscall.ENOENT
	}

	name := node.kidsArr[idx]
	n := node.kidsMap[name]
	return *n, nil
}

// Number of kids for the node
func NumKids(node *Node) int {
	if node == nil {
		return 0
	}

	return node.numKids
}

// In tree at root, get the node corresponding to prefix string.
// Nil if not found
// XXX fix for multiple slashes (a//b/c)
func getNode(root *Node, prefix string) (*Node, error) {

	node := root
	top := ""
	idx := strings.Index(prefix, "/")
	if idx == -1 {
		top = prefix
		prefix = ""
	} else {
		top = prefix[:idx]
		prefix = prefix[idx+1:]
	}

	log.WithFields(log.Fields{"top": top, "prefix": prefix}).Debug("getNode")

	for prefix != "" {
		if node.Entry.IsDir == false {
			log.WithFields(log.Fields{
				"nodeName": node.Entry.Name,
				"prefix":   prefix,
			}).Error("getNode: Node not a dir")
			return nil, syscall.ENOENT
		}

		if node.Entry.Name != top {
			log.WithFields(log.Fields{"top": top,
				"prefix":   prefix,
				"nodeName": node.Entry.Name,
			}).Error("getNode: Node name not same as top")
			return nil, syscall.ENOENT
		}

		idx := strings.Index(prefix, "/")
		if idx == -1 {
			top = prefix
			prefix = ""
		} else {
			top = prefix[:idx]
			prefix = prefix[idx+1:]
		}

		n, ok := node.kidsMap[top]
		if !ok {
			log.WithFields(log.Fields{
				"nodeName": node.Entry.Name,
				"top":      top,
				"prefix":   prefix,
			}).Error("Node doesnt have top in kids")

			return nil, syscall.EINVAL
		}

		node = n
	}

	return node, nil
}

// Lookup file with prefix in tree rooted at node.
// Returns the node correspondig to file
func Lookup(node *Node, prefix string, file string) (*Node, error) {
	if prefix == "" {
		// Looking up Root
		if node.Entry.Name == file {
			return node, nil
		}
		return nil, syscall.ENOENT
	}

	n, err := getNode(node, prefix)
	if err != nil {
		log.Error("Lookup: getNode error")
		return nil, err
	}
	if n.Entry.IsDir == false {
		log.Errorf("Lookup: Prefix %v, node %v, not a dir", prefix, n)
		return nil, syscall.EINVAL
	}

	k, ok := n.kidsMap[file]
	if !ok {
		log.WithFields(log.Fields{
			"nodeName": n.Entry.Name,
			"prefix":   n.Entry.Prefix,
			"file":     file,
		}).Error("Node doesnt have file as kid")
		return nil, syscall.ENOENT
	}

	return k, nil
}

// Insert entry into tree rooted at root
func Insert(root *Node, entry horcrux.Entry) error {
	if root.Entry.IsDir == false {
		log.Error("Insert: cannot insert for non dir")
		return syscall.EINVAL
	}

	file := entry.Name
	prefix := entry.Prefix

	node, err := getNode(root, prefix)
	if err != nil {
		log.Error("Insert: getNode returned empty")
		return err
	}

	if node.Entry.IsDir == false {
		log.WithFields(log.Fields{"Node Name": node.Entry.Name}).Error("Insert: Cannot insert into non-dir node")
		return syscall.EINVAL
	}

	if _, ok := node.kidsMap[file]; ok {
		log.WithFields(log.Fields{"Node": node.Entry.Name,
			"Node prefix":  node.Entry.Prefix,
			"Entry Prefix": entry.Prefix,
			"File":         file,
		}).Error("Insert duplicate entry")
		return syscall.EEXIST
	}

	newNode := &Node{Entry: entry, numKids: 0}
	if entry.IsDir {
		newNode.kidsMap = make(map[string]*Node, MIN_KIDS)
		newNode.kidsArr = make([]string, 0, MIN_KIDS)
	}

	node.kidsMap[file] = newNode
	node.kidsArr = append(node.kidsArr, file)
	node.numKids += 1

	log.WithFields(log.Fields{"Node": node.Entry.Name,
		"Node num kids": node.numKids,
		"Node prefix":   node.Entry.Prefix,
		"Entry Prefix":  entry.Prefix,
		"File":          file,
	}).Debug("Inserted File successfully")
	return nil
}

// Update an entry in tree at root
func Update(root *Node, old horcrux.Entry, new horcrux.Entry) error {
	n, err := Lookup(root, old.Prefix, old.Name)
	if err != nil {
		return err
	}

	if n.Entry != old {
		log.Fatal("Lookup Entry returned bad node")
	}

	(*n).Entry = new
	return nil
}

// Delete dir/file with prefix dir in tree at root
func Delete(root *Node, dir string, file string, isDir bool) (*horcrux.Entry, error) {
	var dirPrefix, dirName string

	log.WithFields(log.Fields{"Root": root.Entry.Name, "Dir": dir, "File": file}).Debug("Delete:")

	sl := strings.Index(dir, "/")
	if sl == -1 {
		dirPrefix = ""
		dirName = root.Entry.Name
	} else {
		dirPrefix = path.Dir(dir)
		dirName = path.Base(dir)
	}

	n, err := Lookup(root, dirPrefix, dirName)
	if err != nil {
		return nil, err
	}

	var i int
	for i = 0; i < n.numKids; i++ {
		if n.kidsArr[i] == file {
			break
		}
	}

	if i == n.numKids {
		log.WithFields(log.Fields{
			"Node pref": n.Entry.Prefix, "Node name": n.Entry.Name, "File": file,
		}).Error("Delete: Node doesn't have file")
		return nil, syscall.ENOENT
	}

	k := n.kidsMap[file]
	if isDir {
		if k.Entry.IsDir == false {
			log.Infof("Delete: entry %v is not a dir", k.Entry.Name)
			return nil, syscall.EINVAL
		}
		if k.numKids != 0 {
			log.Infof("Delete: DIR entry %v is not empty", k.Entry.Name)
			return nil, syscall.ENOTEMPTY
		}
	}

	n.kidsArr = append(n.kidsArr[:i], n.kidsArr[i+1:]...)
	delete(n.kidsMap, file)
	n.numKids--

	return &k.Entry, nil
}

// Create a dirTree from the flat Meta info
func Create(Meta *horcrux.Meta) (*Node, error) {
	if Meta == nil || len(Meta.Entries) == 0 {
		return nil, syscall.EINVAL
	}

	rootEnt := Meta.Entries[0]
	kidsMap := make(map[string]*Node, MIN_KIDS)
	kidsArr := make([]string, 0, MIN_KIDS)
	rootNode := &Node{Entry: rootEnt, kidsMap: kidsMap, kidsArr: kidsArr, numKids: 0}

	for i, entry := range Meta.Entries[1:] {
		log.WithFields(log.Fields{"Prefix": entry.Prefix, "Name": entry.Name}).Debug("Going to insert")
		err := Insert(rootNode, entry)
		if err != nil {
			log.WithFields(log.Fields{
				"Prefix": entry.Prefix,
				"Name":   entry.Name,
				"Index":  i,
			}).Error("Cannot insert")
			return nil, err
		}
	}

	return rootNode, nil
}
