//
// Access interface
// - Provides back end chunk read/write interface
//
package accio

type Access interface {
	Init() (string, error)
	Name() string
	GetFile(src string, dst string) error
}
