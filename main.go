package main

import (
	"flag"
	"fmt"
	"gopkg.in/fsnotify.v1"
	"io/ioutil"
	"log"
	"mime"
	"net/http"
	"os"
    "io"
	"path/filepath"
	"runtime"
	"strings"
    "html/template"
    "regexp"
    
	"github.com/omeid/livereload"
	"github.com/russross/blackfriday"
)

const (
	templateup = `
<!DOCTYPE html>
<html>
<head>
<meta charset="UTF-8">
<title>{{.Title}}</title>
<link rel="stylesheet" href="/_assets/sanitize.css" media="all">
<link rel="stylesheet" href="/_assets/github-markdown.css" media="all">
<link rel="stylesheet" href="/_assets/sons-of-obsidian.css" media="all">
<link rel="stylesheet" href="/_assets/style.css" media="all">
<script src="/_assets/jquery-2.1.1.min.js"></script>
<script src="/_assets/prettify.min.js"></script>
<script>
$(function() {
	$('pre>code').each(function() { $(this.parentNode).addClass('prettyprint') }); prettyPrint();
	$.getScript(window.location.protocol + '//' + window.location.hostname + ':35729/livereload.js');
});
</script>
<style>
.menu {
	 width: 980px;
	 position:relative;
	 margin: 20px auto;
}
.container {
	 width: 980px;
	 border-radius:3px;
	 box-sizing: border-box;
	 border: 1px solid #cccccc;
	 position:relative;
	 padding: 30px 50px;
	 margin: 20px auto;
}
</style>
</head>
<body>
<div class="menu markdown-body">
{{range $var := .Dirnests}}
 <a href="{{$var.Path}}">{{$var.Name}}</a>/
{{end}}
</div>
<div class="container">
<div class="markdown-body">
{{if .Dirdisp}}
<h4>Directory</h4>
<ul>
{{range $var1 := .Dirs}}
 <li><a href="{{$var1}}">{{$var1 | basename}}/</a></li>
{{end}}
</ul>
<h4>Markdown</h4>
<ul>
{{range $var2 := .Files}}
 <li><a href="{{$var2}}">{{$var2 | basename}}</a></li>
{{end}}
</ul>
{{end}}
{{if .CodeFileDisp}}
<pre><code>
{{.CodeText}}
</code><pre>
{{end}}
`
	templatedown = `</div>
</div>
</body>
</html>
`
	extensions = blackfriday.EXTENSION_NO_INTRA_EMPHASIS |
		blackfriday.EXTENSION_TABLES |
		blackfriday.EXTENSION_FENCED_CODE |
		blackfriday.EXTENSION_AUTOLINK |
		blackfriday.EXTENSION_STRIKETHROUGH |
		blackfriday.EXTENSION_SPACE_HEADERS
)

var (
	addr = flag.String("http", ":8000", "HTTP service address (e.g., ':8000')")
)

type DirNest struct {
    Path string
    Name string
}

type Page struct {
    Title string
    Dirnests []DirNest
    Dirdisp bool
    Dirs []string
    Files []string
    CodeFileDisp bool
    CodeText string
}

type String string

func Match(regex string, subject string) bool {
    r := regexp.MustCompile(regex)
	return r.MatchString(subject)
}	

func ReplaceAll(regex, replace, subject string) string {
    r := regexp.MustCompile(regex)
	subject = r.ReplaceAllString(subject, replace)
	return subject
}

func MenuDir(rd string, page *Page) {
    dn := DirNest{"/", "[TOP]"}
    (*page).Dirnests = append((*page).Dirnests, dn)

    if Match("^[\\/\\.]$", rd) {
        return
    }

    rd = ReplaceAll("^\\/", "", rd)
    dirs := strings.Split(rd, "/")
    nwd := ""
    for _, sd := range dirs {
        dn := DirNest{}
        nwd = nwd + "/" + sd;
        dn.Path = nwd
        dn.Name = sd
        (*page).Dirnests = append((*page).Dirnests, dn)
    }
}

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())
	flag.Parse()
	cwd, _ := os.Getwd()

	lrs := livereload.New("mkup")
	defer lrs.Close()

    go func() {
		mux := http.NewServeMux()
		mux.HandleFunc("/livereload.js", func(w http.ResponseWriter, r *http.Request) {
			b, err := Asset("_assets/livereload.js")
			if err != nil {
				http.Error(w, "404 page not found", 404)
				return
			}
			w.Header().Set("Content-Type", "application/javascript")
			w.Write(b)
			return
		})
		mux.Handle("/", lrs)
		log.Fatal(http.ListenAndServe(":35729", mux))
	}()

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		panic(err)
	}

	go func() {
		fsw.Add(cwd)
		err = filepath.Walk(cwd, func(path string, info os.FileInfo, err error) error {
			if info == nil {
				return err
			}
			if !info.IsDir() {
				return nil
			}
			fsw.Add(path)
			return nil
		})

		for {
			select {
			case event := <-fsw.Events:
				if path, err := filepath.Rel(cwd, event.Name); err == nil {
					path = "/" + filepath.ToSlash(path)
					log.Println("reload", path)
					lrs.Reload(path, true)
				}
			case err := <-fsw.Errors:
				if err != nil {
					log.Println(err)
				}
			}
		}
	}()

    funcMap := template.FuncMap{
        "basename" : filepath.Base,
    }
    tpl, err := template.New("foo").Funcs(funcMap).Parse(templateup)
    if err != nil {
        panic(err)
    }

    http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Path

        if strings.HasPrefix(name, "/_assets/") {
			b, err := Asset(name[1:])
			if err != nil {
				http.Error(w, "404 page not found", 404)
				return
			}

			w.Header().Set("Content-Type", mime.TypeByExtension(filepath.Ext(name)))
			w.Write(b)
			return
		}

        fp := filepath.Join(cwd, name)
        info, err := os.Stat(fp)
        if err != nil {
            http.Error(w, "404 page not found", 404)
            return
        }
        
        ext := filepath.Ext(name)
		if ext != ".md" && ext != ".mkd" && ext != ".markdown" {
            if info.IsDir() {
                // index.md redirect
                fim := filepath.Join(fp, "index.md")
                _, err := os.Stat(fim)
                if err == nil {
                    rd, _ := filepath.Rel(cwd, fim)
                    http.Redirect(w, r, "/"+rd, http.StatusFound)
                    return
                }

                page := Page{}
                dir := fp
                page.Dirdisp = true
                page.Title = name + " - mkup"

                // 階層メニュー Dirnests
                rd, _ := filepath.Rel(cwd, dir)
                MenuDir(rd, &page)
                
                files, err := ioutil.ReadDir(dir)
                if err != nil {
                    return
                }
                for _, file := range files {
                    fn := file.Name()
                    if Match("^[\\._]", fn) {
                        continue
                    }
                    
                    fn = filepath.Join(dir, fn)
                    f, err := os.Stat(fn)
                    if err != nil {
                        continue
                    }

                    fn, _ = filepath.Rel(cwd, fn)
                    if f.IsDir() {
                        page.Dirs = append(page.Dirs, "/" + fn);
                    } else {
                        if Match("\\.(md|markdown|mkd)$", fn) {
                            page.Files = append(page.Files, "/" + fn);
                        }
                    }
                }

                w.Header().Set("Content-Type", "text/html; charset=utf-8")
                err = tpl.Execute(w, page)
                if err != nil {
                    panic(err)
                }
                fmt.Fprint(w, templatedown)
                return 
            } else {
                ext = strings.ToLower(ext)
                if Match("(\\.jpg|\\.gif|\\.png)", ext) {
                    rfp, err := os.Open(filepath.Join(cwd, name))
                    if err != nil {
                        http.Error(w, "404 page not found", 404)
                        return
                    }
                    defer rfp.Close()
                    io.Copy(w, rfp)
                } else {
                    page := Page{}
                    page.Title = name + " - mkup"
                    page.CodeFileDisp = true
                    dir := filepath.Dir(fp)
                    
                    // 階層メニュー Dirnests
                    rd, _ := filepath.Rel(cwd, dir)
                    MenuDir(rd, &page)

                    b, err := ioutil.ReadFile(filepath.Join(cwd, name))
                    page.CodeText = string(b)

                    w.Header().Set("Content-Type", "text/html; charset=utf-8")
                    err = tpl.Execute(w, page)
                    if err != nil {
                        panic(err)
                    }
                    fmt.Fprint(w, templatedown)
                }
                return
            }
		}
		b, err := ioutil.ReadFile(filepath.Join(cwd, name))
		if err != nil {
			if os.IsNotExist(err) {
				http.Error(w, "404 page not found", 404)
				return
			}
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		renderer := blackfriday.HtmlRenderer(0, "", "")
		b = blackfriday.Markdown(b, renderer, extensions)

        page := Page{}
        page.Title = filepath.Base(name) + " - mkup"

        // 階層メニュー Dirnests
        rd := filepath.Dir(name)
        MenuDir(rd, &page)
        
        err = tpl.Execute(w, page)
        if err != nil {
            panic(err)
        }
        w.Write(b)
        fmt.Fprint(w, templatedown)
 	})

	server := &http.Server{
		Addr: *addr,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log.Printf("%s %s %s", r.RemoteAddr, r.Method, r.URL.RequestURI())
			http.DefaultServeMux.ServeHTTP(w, r)
		}),
	}

	fmt.Fprintln(os.Stderr, "Listening at "+*addr)
	log.Fatal(server.ListenAndServe())
}
