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
func UploadMap(w http.ResponseWriter, r *http.Request) {
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
		fmt.Println("jpg")
		processFile(w, file, handle)

	case "image/png":
		fmt.Println("png")
		processFile(w, file, handle)

	default:
		jsonResponse(w, http.StatusBadRequest, "The format file is not valid.")
	}

}

func processFile(w http.ResponseWriter, file multipart.File, handle *multipart.FileHeader) {
	data, err := ioutil.ReadAll(file)
	if err != nil {
		fmt.Fprintf(w, "%v", err)
		return
	}

	// write file to tmp
	err = ioutil.WriteFile("./tmp/"+handle.Filename, data, 0777)

	fmt.Println("Filename: ", handle.Filename)

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
	}

	err = generateMbtiles(w, "./tmp/"+tiffOut, mbtilesOut)
	if err != nil {
		log.Fatalf("Could not convert tif to mbtiles file: %v", err)
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

	// remove file from tmp directory
	_ = os.Remove("./tmp/" + handle.Filename + ".tif")
	_ = os.Remove("./tmp/" + handle.Filename + ".mbtiles")
	_ = os.Remove("./tmp/" + handle.Filename)

	// jsonResponse(w, http.StatusOK, "File successfully processed!")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json := json.NewEncoder(w)

	json.Encode("File Processed Successfully")

}

func jsonResponse(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	fmt.Fprint(w, message)
}

// SeasonHandler handles season routes
func SeasonHandler(ef func(error)) http.Handler {
	m := http.NewServeMux()

	m.HandleFunc("/season/map/new", UploadMap)

	return m
}
