package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/lukeroth/gdal"
)

// handle image upload
// process image (geo ref functions)
// move mbtiles file into "./tilesets" directory

var gdalOptions = []string{
	"-of",
	"GTiff",
	"-a_nodata",
	"-9999",
	"-A_ullr",
	"37.5600008",
	"-2.9000001",
	"37.9999873",
	"-3.2500233",
	"-a_srs",
	"+proj=longlat",
	"+ellps=WGS84",
	"+datum=WGS84",
}

var mbtilesOptions = []string{
	"-of",
	"mbtiles",
}

type uploadResponse struct {
	Message string `json:"message"`
	Preview string `json:"preview"`
}

func geoRefImage(w http.ResponseWriter, imgSrc string, outSrc string) error {
	ds, err := gdal.Open(imgSrc, gdal.ReadOnly)
	if err != nil {
		log.Fatal("Could not open dataset: ", err)
		return errors.New("Could not process dataset")
	}

	outputDs := gdal.GDALTranslate("./tmp/"+outSrc, ds, gdalOptions)

	defer outputDs.Close()

	fmt.Println("File generated successfully.")

	// jsonResponse(w, http.StatusContinue, "Geo referenced file, moving along now...")

	return nil
}

func generateMbtiles(w http.ResponseWriter, inputSrc string, outSrc string) error {
	mbtiles, err := gdal.Open(inputSrc, gdal.ReadOnly)
	if err != nil {
		log.Fatal("Could not open data set: ", err)
		return errors.New("Could not process tif to mbtiles dataset")
	}

	outputTiles := gdal.GDALTranslate("./tmp/"+outSrc, mbtiles, mbtilesOptions)

	defer outputTiles.Close()

	gdaladdo := exec.Command("gdaladdo", "./tmp/"+outSrc, "2", "4", "8", "16")

	err = gdaladdo.Run()
	if err != nil {
		log.Fatalf("gladaddo command fail: %v", err)
	}

	//jsonResponse(w, http.StatusOK, "Converted map into an mbtiles format...moving forward")

	return nil
}

// UploadMap handeles map image upload
func (s *ServiceSet) UploadMap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		// better to return a response here
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	file, handle, err := r.FormFile("season_image")
	if err != nil {
		fmt.Fprintf(w, "%v", err)
		return
	}

	defer file.Close()

	mimetype := handle.Header.Get("Content-Type")
	switch mimetype {
	case "image/jpeg":
		s.processFile(w, file, handle)

	case "image/png":
		s.processFile(w, file, handle)

	default:
		jsonResponse(w, http.StatusBadRequest, "The format file is not valid.")
	}

}

func (s *ServiceSet) processFile(w http.ResponseWriter, file multipart.File, handle *multipart.FileHeader) {
	data, err := ioutil.ReadAll(file)
	if err != nil {
		fmt.Fprintf(w, "%v", err)
		return
	}

	// write file to tmp
	err = ioutil.WriteFile("./tmp/"+handle.Filename, data, 0777)

	if err != nil {
		fmt.Fprintf(w, "%v", err)
		return
	}

	// call gdal functions
	tiffOut := handle.Filename + ".tif"
	mbtilesOut := handle.Filename + ".mbtiles"

	err = geoRefImage(w, "./tmp/"+handle.Filename, tiffOut)
	if err != nil {
		log.Fatalf("error geo referencing uploaded image: %v", err)
		jsonResponse(w, http.StatusBadRequest, "Error geo referencing uploaded image")
	}

	err = generateMbtiles(w, "./tmp/"+tiffOut, mbtilesOut)
	if err != nil {
		log.Fatalf("Could not convert tif to mbtiles file: %v", err)
		jsonResponse(w, http.StatusBadRequest, "Could not convert image into tilemap")
	}

	// mv mbtiles file to "tilesets" directory
	mbtilesfile, _ := os.Open("./tmp/" + handle.Filename + ".mbtiles")
	defer mbtilesfile.Close()

	dst, _ := os.Create("./tilesets/" + handle.Filename + ".mbtiles")
	defer dst.Close()

	_, err = io.Copy(dst, mbtilesfile)
	if err != nil {
		log.Fatalf("Could not copy file: %v", err)
	}

	// remove files from tmp directory
	_ = os.Remove("./tmp/" + handle.Filename + ".tif")
	_ = os.Remove("./tmp/" + handle.Filename + ".mbtiles")
	_ = os.Remove("./tmp/" + handle.Filename)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json := json.NewEncoder(w)

	u := uploadResponse{
		Message: "File Processed Succcessfully",
		Preview: handle.Filename,
	}

	json.Encode(u)

}

func gracefulReload() {
	graceful := exec.Command("killall", "-HUP", "mbtileserver")
	err := graceful.Run()
	if err != nil {
		log.Fatalf("graceful reload command fail: %v", err)
	}
}

func (s *ServiceSet) reloadNewServiceSet(baseDir string, secretKey string) {

	// clear map
	for k := range s.tilesets {
		delete(s.tilesets, k)
	}

	var filenames []string
	err := filepath.Walk(baseDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if ext := filepath.Ext(p); ext == ".mbtiles" {
			filenames = append(filenames, p)
		}
		return nil
	})
	if err != nil {
		log.Fatalf("unable to scan tilesets: %v", err)
	}

	newSet := New()
	newSet.secretKey = s.secretKey

	for _, filename := range filenames {
		subpath, err := filepath.Rel(baseDir, filename)
		if err != nil {
			log.Fatalf("unable to extract URL path for %q: %v", filename, err)
		}
		e := filepath.Ext(filename)
		p := filepath.ToSlash(subpath)
		id := strings.ToLower(p[:len(p)-len(e)])
		err = s.AddDBOnPath(filename, id)
		if err != nil {
			log.Fatal(err)
		}
	}
}

func jsonResponse(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	fmt.Fprint(w, message)
}

func (s *ServiceSet) reloadTiles(w http.ResponseWriter, r *http.Request) {

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json := json.NewEncoder(w)

	json.Encode("Reloaded tiles")

	go gracefulReload()
}

// SeasonHandler handles season routes
func (s *ServiceSet) SeasonHandler(ef func(error)) http.Handler {
	m := http.NewServeMux()

	m.HandleFunc("/season/map/new", s.UploadMap)

	return m
}

func (s *ServiceSet) ReloadTilesHandler(ef func(error)) http.Handler {
	m := http.NewServeMux()

	m.HandleFunc("/reload/tiles", s.reloadTiles)

	return m
}
