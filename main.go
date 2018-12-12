package main

/**
This programme is created as a util tool / API service for image resizing, rotation and cropping
with the possibility of creating additional sizes / thumbnails of the formatted image.

Order of actions: rotation, cropping, resizing
*/

import (
	"encoding/json"
	"flag"
	"image"
	"image/color"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/disintegration/imaging"
	"github.com/gorilla/mux"
)

func main() {
	var (
		help    = flag.Bool("help", false, "Displays help text.")
		api     = flag.Bool("api", false, "Runs the script as a Web API. Requires a port to be specified.")
		root    = flag.String("root", ".", "Root folder to store the processed images by the Web API. Default: .")
		port    = flag.String("port", "", "The port to be used if the script would be run as a Web API.")
		src     = flag.String("src", "", "Source image.")
		dst     = flag.String("dst", "", "Destination of new image.")
		cropx   = flag.Int("cropx", 0, "X coordinate to start crop.")
		cropy   = flag.Int("cropy", 0, "Y coordinate to start crop.")
		cropw   = flag.Int("cropw", 0, "Width of crop.")
		croph   = flag.Int("croph", 0, "Height of crop.")
		rotate  = flag.Float64("rotate", 0, "Degrees rotation.")
		fill    = flag.String("fill", "black", "Color to fill: black / b, white / w. Default: transparent.")
		resizew = flag.Int("resizew", 0, "Resize width. If 0, ratio will be preserved.")
		resizeh = flag.Int("resizeh", 0, "Resize height. If 0, ratio will be preserved.")
	)

	flag.Parse()

	if *help {
		flag.Usage()
		return
	}

	if *api {
		startAPI(*port, *root)
		return
	}

	options := Options{
		Crop: Crop{
			X:      *cropx,
			Y:      *cropy,
			Width:  *cropw,
			Height: *croph,
		},
		Rotate: *rotate,
		Fill:   *fill,
		Resize: Resize{
			Width:  *resizew,
			Height: *resizeh,
		},
		Thumbnails: []Thumb{
			Thumb{
				Suffix: "-small",
				Width:  150,
				Height: 150,
			},
		},
	}

	startScript(*src, *dst, &options)
}

func startAPI(port string, root string) {
	r := mux.NewRouter()

	r.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	r.HandleFunc("/format", handleFormatRequest(root)).Methods("POST")

	http.Handle("/", r)

	if !strings.HasPrefix(port, ":") {
		port = ":" + port
	}

	log.Printf("Listening on %s\n", port)
	log.Println(http.ListenAndServe(port, nil))
}

func handleFormatRequest(root string) func(http.ResponseWriter, *http.Request) {
	log.Printf("Root dir: %s\n", root)
	var maxMem int64 = 2 * 1024 * 1024 // 2MB

	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		if err != nil && !os.IsNotExist(err) {
			log.Fatalln(err)
		}
		if info != nil && !info.IsDir() {
			log.Fatalf("%s is not a directory!", root)
		}
	}

	if err := os.Mkdir(root, os.ModeDir); err != nil && !os.IsExist(err) {
		log.Fatalln(err)
	}

	return func(w http.ResponseWriter, r *http.Request) {
		r.ParseMultipartForm(maxMem)

		_, h, err := r.FormFile("image")
		name := r.FormValue("name")
		optionsJSON := r.FormValue("options")

		log.Println(optionsJSON)

		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(err.Error()))
			return
		}

		img, err := h.Open()
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(err.Error()))
			return
		}

		log.Println("Reading options...")
		options := Options{}
		err = json.Unmarshal([]byte(optionsJSON), &options)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(err.Error()))
			return
		}

		_filepath := filepath.Join(root, getThumbName(name, "-original"))
		log.Println("Saving original: %s", _filepath)
		outfile, err := os.Create(_filepath)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(err.Error()))
			return
		}

		if _, err = io.Copy(outfile, img); nil != err {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(err.Error()))
			return
		}

		log.Println("Opening original...")
		srcImg, err := imaging.Open(_filepath)
		if err != nil {
			log.Printf("Failed to open image: %s", err)
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(err.Error()))
			return
		}

		log.Println("Processing...")
		result := processImage(name, &srcImg, &options)

		response := APIResponse{
			Original: filepath.ToSlash(_filepath),
		}

		for i, r := range *result {
			thumbPath := filepath.Join(root, r.Name)
			log.Printf("Saving image %s\n", thumbPath)
			err = imaging.Save(*r.Image, thumbPath)

			if err != nil {
				log.Printf("Failed to save image: %s", err)
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(err.Error()))
				return
			}

			thumbPath = filepath.ToSlash(thumbPath)
			if i == 0 {
				response.Formatted = thumbPath
			} else {
				response.Thumbnails = append(response.Thumbnails, thumbPath)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}
}

func startScript(src string, dest string, options *Options) {

	srcImg, err := imaging.Open(src)
	if err != nil {
		log.Fatalf("Failed to open image: %v", err)
	}

	result := processImage(dest, &srcImg, options)

	for _, r := range *result {
		log.Printf("Saving image %s\n", r.Name)
		err = imaging.Save(*r.Image, r.Name)

		if err != nil {
			log.Fatalf("Failed to save image: %v", err)
		}
	}
}

type Options struct {
	Crop       Crop    `json:"crop,omitempty"`
	Rotate     float64 `json:"rotate,omitempty"`
	Fill       string  `json:"fill,omitempty"`
	Resize     Resize  `json:"resize,omitempty"`
	Thumbnails []Thumb `json:"thumbnails,omitempty"`
}

type Crop struct {
	X      int `json:"x,omitempty"`
	Y      int `json:"y,omitempty"`
	Width  int `json:"width,omitempty"`
	Height int `json:"height,omitempty"`
}

func (c *Crop) shouldCrop(img *image.Image) bool {
	size := (*img).Bounds().Size()
	return c.X != 0 || c.Y != 0 ||
		(c.Width > 0 && c.Height > 0 && (c.Width != size.X || c.Height != size.Y))
}

type Resize struct {
	Width  int `json:"width,omitempty"`
	Height int `json:"height,omitempty"`
}

type Thumb struct {
	Suffix string `json:"suffix,omitempty"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

type ProcessedImage struct {
	Name  string
	Image *image.Image
}

type APIResponse struct {
	Formatted  string   `json:"formatted,omitempty"`
	Original   string   `json:"original,omitempty"`
	Thumbnails []string `json:"thumbnails,omitempty"`
}

func processImage(name string, src *image.Image, options *Options) *[]ProcessedImage {

	images := make([]ProcessedImage, 1)

	src = rotate(src, options.Rotate, options.Fill)
	src = crop(src, &options.Crop)
	src = resize(src, options.Resize.Width, options.Resize.Height)

	images[0] = ProcessedImage{
		Name:  name,
		Image: src,
	}

	if options.Thumbnails != nil {
		for _, t := range options.Thumbnails {
			thumbName := getThumbName(name, t.Suffix)
			thumbImg := resize(src, t.Width, t.Height)
			images = append(images, ProcessedImage{
				Name:  thumbName,
				Image: thumbImg,
			})
		}
	}

	return &images
}

func getThumbName(name string, suffix string) string {
	ext := filepath.Ext(name)
	base := string(name[0 : len(name)-len(ext)])
	return base + suffix + ext
}

func rotate(img *image.Image, deg float64, fill string) *image.Image {
	if deg == 0 {
		return img
	}
	fill = strings.ToLower(fill)
	var c color.Color = color.Transparent
	if strings.Compare(fill, "black") == 0 || strings.Compare(fill, "b") == 0 {
		c = color.Black
	} else if strings.Compare(fill, "white") == 0 || strings.Compare(fill, "w") == 0 {
		c = color.White
	}
	log.Printf("Rotating %f degrees. Fill color: %s\n", deg, c)
	var result image.Image = imaging.Rotate(*img, deg, c)
	return &result
}

func crop(img *image.Image, crop *Crop) *image.Image {
	if !crop.shouldCrop(img) {
		return img
	}

	var (
		w = crop.X + crop.Width
		h = crop.Y + crop.Height
	)

	log.Printf("Cropping: x = %d, y = %d, w = %d, h = %d.\n", crop.X, crop.Y, w, h)
	var result image.Image = imaging.Crop(*img, image.Rect(crop.X, crop.Y, w, h))
	return &result
}

func resize(img *image.Image, w int, h int) *image.Image {
	if w <= 0 && h <= 0 {
		return img
	}
	if w == 0 {
		w = h
	} else if h == 0 {
		h = w
	}
	size := (*img).Bounds().Size()
	if size.X == w && size.Y == h {
		return img
	}
	log.Printf("Resizing: w = %d, h = %d.\n", w, h)
	var result image.Image = imaging.Resize(*img, w, h, imaging.Lanczos)
	return &result
}
