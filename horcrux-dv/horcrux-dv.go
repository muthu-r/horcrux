/*
 * TOCHECK
 *	- Why scp doesnt bind mount first time
 */
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/docker/go-connections/sockets"
	log "github.com/Sirupsen/logrus"

	"github.com/muthu-r/horcrux"
	"github.com/muthu-r/horcrux/revelo"
)

const (
	DV_WORKDIR       = "/run/horcrux"
	DV_VERSION       = horcrux.VERSION
    DV_PRESERVED_DIR = "/run/horcrux"
	DV_VOL_LIST_FILE = "vols.lst"
	DV_VOL_MIN       = 100
	DV_SOCK_PATH	 = "/run/docker/plugins"
	DV_SOCK_NAME	 = DV_SOCK_PATH + "/" + "horcrux.sock"
//	DV_TCP_PORT      = "9090"
//	DV_HOST          = "localhost"
)

type Volume struct {
	DvName     string `json:"Docker Name"`  // Docker Vol name (docker volume create --name)
	HorName    string `json:"Horcrux Name"` // Horcrux volume name - separate from dvname
	AccessArgs string `json:"AccessArgs"`   // Access specific args
	mntCount   int    // Number of times mounted
	MntDir     string `json:"Mount Dir"` // Mount dir for volume - from WORKDIR and horname
	CacheDir   string `json:"Cache Dir"` // Cache dir
}

type VolumeData struct {
	Version     string            `json:"Version"`
	Volumes     map[string]Volume `json:"Volumes"`
	volFileName string
	lock        sync.RWMutex //XXX Do we need this initialized? Seems not...
}

var VolData VolumeData = VolumeData{Version: DV_VERSION}

func listAllVols() {
	log.Debugf("===== Vol List ====")
	for i, v := range VolData.Volumes {
		log.Debugf("%v: %v", i, v)
	}
}

func storeVols() error {
	log.Info("Storing volume list...")

	VolData.lock.RLock()
	js, err := json.MarshalIndent(VolData, "", "    ")
	VolData.lock.RUnlock()

	volFile, err := os.OpenFile(VolData.volFileName, syscall.O_WRONLY|syscall.O_CREAT, 0600)
	if err != nil {
		log.Error("DV: Cannot open for writing, file: " + DV_VOL_LIST_FILE)
		return err
	}
	defer volFile.Close()

	for err := syscall.Flock(int(volFile.Fd()), syscall.LOCK_EX); err == syscall.EINTR; {
	}
	defer syscall.Flock(int(volFile.Fd()), syscall.LOCK_UN)

	volFile.Truncate(0)

	_, err = volFile.Write(js)
	if err != nil {
		log.Errorf("Cannot write vol file - %v", err)
		return err
	}

	return nil
}

func readVols() error {
	log.Infof("Reading volume list...")

	volFile, err := os.Open(VolData.volFileName)
	if err != nil {
		log.Errorf("Cannot open vol file - %v", err)
		return err
	}
	defer volFile.Close()

	for err := syscall.Flock(int(volFile.Fd()), syscall.LOCK_SH); err == syscall.EINTR; {
	}

	st, _ := volFile.Stat()
	volData := make([]byte, st.Size()+1)
	n, err := volFile.Read(volData)
	if err != nil || n == 0 {
		log.Errorf("Cannot read from vol file - %v", err)
		syscall.Flock(int(volFile.Fd()), syscall.LOCK_UN)
		return err
	}
	syscall.Flock(int(volFile.Fd()), syscall.LOCK_UN)

	volData = volData[:n]

	VolData.lock.Lock()
	err = json.Unmarshal(volData, &VolData)
	VolData.lock.Unlock()

	if err != nil {
		log.Errorf("Read vols, cannot unmarshal vol list - %v", err)
		return err
	}

	return nil
}

func createWorkDirs(name string) (string, string, error) {

	cd := DV_WORKDIR + "/" + name
	err := os.MkdirAll(cd, 0700) //XXX Revisit permission
	if err != nil {
		return "", "", err
	}

	md := DV_WORKDIR + "/mnt" + name
	err = os.MkdirAll(md, 0700) //XXX Revisit permission
	if err != nil {
		return "", "", err
	}

	return cd, md, nil
}

//
// Docker API implementation
//
const (
	contentType = `application/vnd.docker.plugins.v1+json`
	implements  = `{"Implements":["VolumeDriver"]}`
)

type DockerRequest struct {
	Name    string            `json:"Name"`
	Options map[string]string `json:"Opts, omitempty"`
}

type DockerVolume struct {
	Name		string		`json:"Name"`
	MntPoint	string		`json:"Mountpoint"`
}

type Capability struct {
    Scope       string      `json: "Scope"`
}

type DockerResponse struct {
	MntPoint	string		    `json:"Mountpoint"`
	Volume		DockerVolume	`json:"Volume"`
	VolumeList	[]DockerVolume	`json:"Volumes"`
	Caps        Capability      `json:"Capabilities"`
	Err			string			`json:"Err"`
}

func getDockerRequest(w http.ResponseWriter, r *http.Request) (*DockerRequest, error) {
	req := new(DockerRequest)
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil && err != io.EOF {
		log.Error("dv: Invalid http request")
		http.Error(w, err.Error(), http.StatusBadRequest)
		return nil, err
	}

	return req, nil
}

func putDockerResponse(w http.ResponseWriter, resp *DockerResponse) {
	w.Header().Set("Content-Type", contentType)
	if resp.Err != "" {
		log.Debugf("dv: putDockerResponse - Error: " + resp.Err)
		http.Error(w, resp.Err, http.StatusBadRequest)
		return
	}

	json.NewEncoder(w).Encode(resp)
}

type DockerPluginHandles struct {
	EndPoint string
	Handler  func(req *DockerRequest) *DockerResponse
}

const activateEndPoint = "/Plugin.Activate"

var Handles = []DockerPluginHandles{
	{"/VolumeDriver.Create",	CreateHandler},
	{"/VolumeDriver.Remove",	RemoveHandler},
	{"/VolumeDriver.Mount",		MountHandler},
	{"/VolumeDriver.Unmount",	UnmountHandler},
	{"/VolumeDriver.Path",		PathHandler},
	{"/VolumeDriver.Get",		GetHandler},
	{"/VolumeDriver.List",		ListHandler},
	{"/VolumeDriver.Capabilities",	CapHandler}}

func ActivateHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", contentType)
	fmt.Fprintf(w, "%s", implements)
	log.Debug("dv: Activate Handler")
	return
}

func CreateHandler(req *DockerRequest) *DockerResponse {
	var err error

	log.Debugf("dv: Create Handler: Req %v: ", req)

	VolData.lock.RLock()
	_, ok := VolData.Volumes[req.Name]
	VolData.lock.RUnlock()

	if ok {
		if len(req.Options) == 0 {
			log.Infof("dv: Docker Restart(?), Vol %v already exists", VolData.Volumes[req.Name])
			return &DockerResponse{Err: ""}
		} else {
			log.Errorf("dv: Create: Volume %v already exists", VolData.Volumes[req.Name])
			return &DockerResponse{Err: " Volume " + req.Name + " already exists"}
		}
	}

	v := Volume{DvName: req.Name, HorName: req.Options["--name"], AccessArgs: req.Options["--access"]}

	v.CacheDir, v.MntDir, err = createWorkDirs(v.DvName)
	log.WithFields(log.Fields{"CacheDir": v.CacheDir, "MntDir": v.MntDir}).Debug("dv: Create: ")
	if err != nil {
		log.WithFields(log.Fields{"Name": v.HorName, "Error": err}).Error("dv: Create: Cannot create work dirs")
		return &DockerResponse{Err: "Volume: " + v.DvName + ", Error: " + err.Error()}
	}

	VolData.lock.Lock()
	VolData.Volumes[req.Name] = v
	VolData.lock.Unlock()

	log.Infof("Created volume %v", v)
	listAllVols()
	storeVols()

	return &DockerResponse{Err: ""}
}

func RemoveHandler(req *DockerRequest) *DockerResponse {
	log.WithFields(log.Fields{"Req": req}).Debug("dv: Remove Handler")

	VolData.lock.RLock()
	v, ok := VolData.Volumes[req.Name]
	VolData.lock.RUnlock()

	if !ok {
		log.WithFields(log.Fields{"Volume": v}).Error("dv: Remove: Volume not found")
		return &DockerResponse{Err: " Volume " + v.DvName + " not found"}
	}

	if v.mntCount > 0 {
		log.WithFields(log.Fields{"Volume": v}).Error("dv: Remove: Volume in use")
		return &DockerResponse{Err: " Volume " + v.DvName + " in use"}
	}

	VolData.lock.Lock()
	delete(VolData.Volumes, req.Name)
	VolData.lock.Unlock()

	// Cleanup cache
	// Extra sanity: Make sure we are removing our files :)
	if strings.HasPrefix(v.CacheDir, DV_WORKDIR) {
		os.RemoveAll(v.CacheDir)
		os.Remove(v.MntDir)
	}

	log.Infof("Removed volume %v", v)
	listAllVols()
	storeVols()

	return &DockerResponse{Err: ""}
}

func MountHandler(req *DockerRequest) *DockerResponse {
	log.WithFields(log.Fields{"Req": req}).Debug("dv: Mount Handler")

	VolData.lock.RLock()
	v, ok := VolData.Volumes[req.Name]
	VolData.lock.RUnlock()

	if !ok {
		log.WithFields(log.Fields{"Volume": v}).Error("dv: Mount: Volume not found")
		return &DockerResponse{Err: " Volume " + req.Name + " not found"}
	}

	if v.mntCount > 0 {
		log.WithFields(log.Fields{"Volume": v}).Info("dv: Mount: Volume already mounted")
	}

	if v.mntCount == 0 {
		go func() {
			err := revelo.Revelo(v.HorName, v.AccessArgs, v.CacheDir, v.MntDir)
			if err != nil {
				log.WithFields(log.Fields{"Volume": v, "Error": err}).Error("dv: Mount: Cannot mount")
				return
			}
		}()
	}

	v.mntCount++

	VolData.lock.Lock()
	VolData.Volumes[req.Name] = v
	VolData.lock.Unlock()

	log.Infof("Mounted volume %v", v)
	listAllVols()

	return &DockerResponse{MntPoint: v.MntDir, Err: ""}
}

func UnmountHandler(req *DockerRequest) *DockerResponse {
	log.WithFields(log.Fields{"Req": req}).Debug("dv: UNmount Handler")

	VolData.lock.RLock()
	v, ok := VolData.Volumes[req.Name]
	VolData.lock.RUnlock()

	if !ok {
		log.WithFields(log.Fields{"Volume": v}).Error("dv: UNmount: Volume not found")
		return &DockerResponse{Err: " Volume " + req.Name + " not found"}
	}

	if v.mntCount <= 0 {
		log.WithFields(log.Fields{"Volume": v}).Error("dv: UNmount: Volume not mounted")
		return &DockerResponse{Err: " Volume " + v.DvName + " not mounted"}
	}

	v.mntCount--

	VolData.lock.Lock()
	VolData.Volumes[req.Name] = v
	VolData.lock.Unlock()

	if v.mntCount == 0 {
		err := revelo.Unmount(v.MntDir)
		if err != nil {
			log.WithFields(log.Fields{"Volume": v, "Error": err}).Error("dv: UNmount: Cannot mount")
			return &DockerResponse{Err: " Volume " + v.DvName + " cannot UNmount"}
		}
	}

	log.Infof("UNMounted volume %v", v)
	listAllVols()

	return &DockerResponse{Err: ""}
}

func PathHandler(req *DockerRequest) *DockerResponse {
	VolData.lock.RLock()
	v, ok := VolData.Volumes[req.Name]
	VolData.lock.RUnlock()

	if !ok {
		log.WithFields(log.Fields{"Volume": v}).Error("dv: Path: Volume not found")
		listAllVols()
		return &DockerResponse{Err: " Volume " + req.Name + " not found"}
	}

	log.WithFields(log.Fields{"Req": req, "Volume": v}).Debug("dv: Path Handler")

	log.Infof("Path for volume %v", v)
	return &DockerResponse{MntPoint: v.MntDir}
}

func GetHandler(req *DockerRequest) *DockerResponse {
	VolData.lock.RLock()
	v, ok := VolData.Volumes[req.Name]
	VolData.lock.RUnlock()

	if !ok {
		return &DockerResponse{Err:"Volume " + req.Name + " not found"}
	}

	dv := DockerVolume{Name:v.DvName, MntPoint:v.MntDir}
	return &DockerResponse{Volume:dv, Err:""}
}

func ListHandler(req *DockerRequest) *DockerResponse {
	var dvlist []DockerVolume

	VolData.lock.RLock()
	defer VolData.lock.RUnlock()

	for _, v := range VolData.Volumes {
		dvlist = append(dvlist, DockerVolume{Name:v.DvName, MntPoint:v.MntDir})
	}

	return &DockerResponse{VolumeList:dvlist, Err:""}
}

func CapHandler(req *DockerRequest) *DockerResponse {
    var cap Capability

    cap.Scope = "global"
    return &DockerResponse{Caps:cap, Err:""}
}

func unmountAllVols() {
	for _, v := range VolData.Volumes {
		if v.mntCount > 0 {
			if err := revelo.Unmount(v.MntDir); err != nil {
				log.Errorf("Cannot unmount vol: %v, error: %v", v.DvName, err)
			} else {
				log.Errorf("Unmounted vol: %v", v.DvName)
			}
		}
	}
}

func handleSignals() {
	sigConn := make(chan os.Signal, 10)
	signal.Notify(sigConn, os.Interrupt)
	signal.Notify(sigConn, syscall.SIGTERM)
	signal.Notify(sigConn, syscall.SIGUSR1)
	signal.Notify(sigConn, syscall.SIGUSR2)
	go func() {
		for {
			sig := <-sigConn
			if sig == syscall.SIGUSR1 {
				old := log.GetLevel()
				new := old + 1
				if new > log.DebugLevel {
					new = log.DebugLevel
				}
				if new != old {
					log.SetLevel(new)
					log.WithFields(log.Fields{"old": old, "new": new}).Error("Increased logging")
				}
			} else if sig == syscall.SIGUSR2 {
				old := log.GetLevel()
				new := old - 1
				if new < log.FatalLevel {
					new = log.FatalLevel
				}
				if new != old {
					log.WithFields(log.Fields{"old": old, "new": new}).Error("Decreased logging")
					log.SetLevel(new)
				}
			} else {
				log.Error("Interrupted... exiting")
				unmountAllVols()
				os.Remove(DV_SOCK_NAME)
				os.Exit(1)
			}
		}
	}()
}

func main() {
	log.SetLevel(horcrux.LOGLEVEL)

	err := os.MkdirAll(DV_SOCK_PATH, 0700)

	VolData.volFileName = DV_PRESERVED_DIR + "/" + DV_VOL_LIST_FILE

	VolData.Volumes = make(map[string]Volume, DV_VOL_MIN)
	err = readVols()
	if err != nil {
		log.Errorf("Cannot read volume list - %v", err)
		log.Errorf("Continuing...")
	}

	defer storeVols()

	http.HandleFunc(activateEndPoint, ActivateHandler)
	for i := 0; i < len(Handles); i++ {
		Handler := Handles[i].Handler
		http.HandleFunc(Handles[i].EndPoint, func(w http.ResponseWriter, r *http.Request) {
			req, err := getDockerRequest(w, r)
			_ = req
			if err != nil {
				log.Errorf("Cannot get docker request - err %v", err)
				return
			}
			resp := Handler(req)
			if resp != nil {
				putDockerResponse(w, resp)
			}
			return
		})
	}

	handleSignals()

//	log.WithFields(log.Fields{"Host": DV_HOST, "Port": DV_TCP_PORT}).Info("Listening...")

	log.WithFields(log.Fields{"Socket": DV_SOCK_NAME}).Info("Listening...")
	listener, err := sockets.NewUnixSocket(DV_SOCK_NAME, os.Getgid())
	if err != nil {
		log.Errorf("dv: Cannot create unix socket %v, err %v", DV_SOCK_NAME, err)
		return
	}
	defer os.Remove(DV_SOCK_NAME)

	log.WithFields(log.Fields{"Version": DV_VERSION}).Info("Horcrux Volume plugin started...")

	err = http.Serve(listener, nil)
	if err != nil {
		log.Errorf("dv: Cannot Serve on unix socket, err %v", err)
		return
	}

//	err = http.ListenAndServe(DV_HOST+":"+DV_TCP_PORT, nil)
//	if err != nil {
//		log.Errorf("dv: http server error %v", err)
//	}
//

	return
}
