package main

import (
	"bufio"
	"flag"
	"fmt"
	"gopkg.in/fsnotify.v1"
	"html"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/omeid/livereload"
	"github.com/russross/blackfriday"
	"golang.org/x/exp/utf8string"
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
.right {
	 float: right;
}
input {
     border: 1px solid #cccccc;
}
</style>
</head>
<body>
<div class="menu markdown-body">
{{range $var := .Dirnests}}
 <a href="{{$var.Path}}">{{$var.Name}}</a>/
{{end}}
<div class="right">
<form action="{{.Spath}}" method="post">
<p><input type="text" name="word" size="20"><input type="submit" value="検索"></p>
</form>
</div>
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

type dirNest struct {
	Path string
	Name string
}

type page struct {
	Title        string
	Spath        string
	Dirnests     []dirNest
	Dirdisp      bool
	Dirs         []string
	Files        []string
	CodeFileDisp bool
	CodeText     string
}

// String type string
type String string

// Match regex match
func Match(regex string, subject string) bool {
	r := regexp.MustCompile(regex)
	return r.MatchString(subject)
}

// ReplaceAll regex replace
func ReplaceAll(regex, replace, subject string) string {
	r := regexp.MustCompile(regex)
	subject = r.ReplaceAllString(subject, replace)
	return subject
}

// MenuDir page top header
func MenuDir(rd string, pg *page) {
	// (*pg).Spath = filepath.Join("/_search", rd) + "/"
	sp := filepath.Join("/_search", rd) + "/"
	(*pg).Spath = ReplaceAll(`\\`, "/", sp)

	dn := dirNest{"/", "[TOP]"}
	(*pg).Dirnests = append((*pg).Dirnests, dn)

	if Match("^[\\/\\.]$", rd) {
		return
	}

	rd = ReplaceAll(`\\`, "/", rd)
	rd = ReplaceAll("^\\/", "", rd)
	dirs := strings.Split(rd, "/")
	nwd := ""
	for _, sd := range dirs {
		dn := dirNest{}
		nwd = nwd + "/" + sd
		dn.Path = nwd
		dn.Name = sd
		(*pg).Dirnests = append((*pg).Dirnests, dn)
	}
}

func filepathRel(base, dir string) (rd string, err error) {
	rd, err = filepath.Rel(base, dir)
	if err != nil {
		return
	}
	rd = ReplaceAll(`\\`, "/", rd)
	log.Println(rd)
	return
}

func fileview(cwd string, w http.ResponseWriter, r *http.Request) {
	name := r.URL.Path
	fp := filepath.Join(cwd, name)

	pg := page{}
	pg.Title = name + " - mkup"
	pg.CodeFileDisp = true
	dir := filepath.Dir(fp)

	// 階層メニュー Dirnests
	rd, _ := filepathRel(cwd, dir)
	MenuDir(rd, &pg)

	b, err := ioutil.ReadFile(filepath.Join(cwd, name))
	pg.CodeText = string(b)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// tpl
	funcMap := template.FuncMap{
		"basename": filepath.Base,
	}
	tpl, err := template.New("foo").Funcs(funcMap).Parse(templateup)
	if err != nil {
		panic(err)
	}
	err = tpl.Execute(w, pg)
	if err != nil {
		panic(err)
	}

	fmt.Fprint(w, templatedown)

	return
}

func mdview(cwd string, w http.ResponseWriter, r *http.Request) {
	name := r.URL.Path
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

	pg := page{}
	pg.Title = filepath.Base(name) + " - mkup"

	// 階層メニュー Dirnests
	rd := filepath.Dir(name)
	MenuDir(rd, &pg)

	// tpl
	funcMap := template.FuncMap{
		"basename": filepath.Base,
	}
	tpl, err := template.New("foo").Funcs(funcMap).Parse(templateup)
	if err != nil {
		panic(err)
	}
	err = tpl.Execute(w, pg)
	if err != nil {
		panic(err)
	}

	w.Write(b)

	fmt.Fprint(w, templatedown)
	return
}

func imageview(cwd string, w http.ResponseWriter, r *http.Request) {
	name := r.URL.Path
	rfp, err := os.Open(filepath.Join(cwd, name))
	if err != nil {
		http.Error(w, "404 page not found", 404)
		return
	}
	defer rfp.Close()
	io.Copy(w, rfp)
	return
}

func dirview(cwd string, w http.ResponseWriter, r *http.Request) {
	name := r.URL.Path
	dir := filepath.Join(cwd, name)

	// index.md redirect
	fim := filepath.Join(dir, "index.md")
	_, err := os.Stat(fim)
	if err == nil {
		rd, _ := filepathRel(cwd, fim)
		http.Redirect(w, r, "/"+rd, http.StatusFound)
		return
	}

	pg := page{}
	pg.Dirdisp = true
	pg.Title = name + " - mkup"

	// 階層メニュー Dirnests
	rd, _ := filepathRel(cwd, dir)
	MenuDir(rd, &pg)

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

		fn, _ = filepathRel(cwd, fn)
		if f.IsDir() {
			pg.Dirs = append(pg.Dirs, "/"+fn)
		} else {
			if Match("\\.(md|markdown|mkd)$", fn) {
				pg.Files = append(pg.Files, "/"+fn)
			}
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// tpl
	funcMap := template.FuncMap{
		"basename": filepath.Base,
	}
	tpl, err := template.New("foo").Funcs(funcMap).Parse(templateup)
	if err != nil {
		panic(err)
	}
	err = tpl.Execute(w, pg)
	if err != nil {
		panic(err)
	}

	fmt.Fprint(w, templatedown)

	return
}

func search(cwd string, w http.ResponseWriter, r *http.Request) {
	name := r.URL.Path
	name = ReplaceAll("^/_search", "", name)
	r.ParseForm()
	word := r.Form.Get("word")

	if len(word) <= 0 {
		http.Redirect(w, r, name, http.StatusFound)
		return
	}
	name = filepath.Join(cwd, name)

	pg := page{}
	pg.Title = "search - mkup"

	// 階層メニュー Dirnests
	rd, _ := filepathRel(cwd, name)
	MenuDir(rd, &pg)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// fmt.Fprintf(w, "search %s  word→%s", name, word)
	path, err := exec.LookPath("ag")

	cmd := &exec.Cmd{}
	asci := utf8string.NewString(word)
	if asci.IsASCII() {
		cmd = exec.Command(path, "-i", word, name)
	} else {
		cmd = exec.Command(path, word, name)
	}
	// fmt.Printf("%s -i %s %s\n", path, word, name)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		http.Error(w, "404 page not found (ag not found)", 404)
		return
	}
	err = cmd.Start()
	if err != nil {
		http.Error(w, "404 page not found (ag not found)", 404)
		return
	}

	// tpl
	funcMap := template.FuncMap{
		"basename": filepath.Base,
	}
	tpl, err := template.New("foo").Funcs(funcMap).Parse(templateup)
	if err != nil {
		panic(err)
	}
	err = tpl.Execute(w, pg)
	if err != nil {
		panic(err)
	}

	fmt.Fprintf(w, "<h2>%s の検索結果</h2><br />\n", word)

	top := cwd
	top = ReplaceAll(`^[a-zA-Z]:`, "", top) // windows c: ...cut
	top = ReplaceAll(`\\`, "/", top)        // windows path \ --> /

	s := bufio.NewScanner(stdout)
	b := ""
	for s.Scan() {
		t := s.Text()
		t = ReplaceAll(`^[a-zA-Z]:`, "", t) // windows c: ...cut
		t = ReplaceAll(`\\`, "/", t)        // windows path \ --> /
		pr := strings.Split(t, ":")
		f, err := filepathRel(top, pr[0])
		log.Printf("%s %s", top, pr[0])
		if err == nil {
			if b != f {
				b = f
				fmt.Fprintf(w, "<a href=\"/%s\">%s</a><br />\n", f, f)
			}
			t := strings.Join(pr[2:], ":")
			fmt.Fprintf(w, "　%v : %s<br />", pr[1], html.EscapeString(t))
		} else {
			fmt.Fprintf(w, "%s<br />\n", t)
		}
	}

	fmt.Fprint(w, templatedown)

	return
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
				if path, err := filepathRel(cwd, event.Name); err == nil {
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

	http.HandleFunc("/_assets/", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Path
		b, err := Asset(name[1:])
		if err != nil {
			http.Error(w, "404 page not found", 404)
			return
		}

		w.Header().Set("Content-Type", mime.TypeByExtension(filepath.Ext(name)))
		w.Write(b)
		return
	})

	http.HandleFunc("/_search/", func(w http.ResponseWriter, r *http.Request) {
		search(cwd, w, r)
		return
	})

	mdext := map[string]bool{".md": true, ".mkd": true, ".markdown": true}
	imgext := map[string]bool{".jpeg": true, ".jpg": true, ".gif": true, ".png": true}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Path

		fp := filepath.Join(cwd, name)
		info, err := os.Stat(fp)
		if err != nil {
			http.Error(w, "404 page not found", 404)
			return
		}

		if info.IsDir() {
			dirview(cwd, w, r)
			return
		} else {
			ext := strings.ToLower(filepath.Ext(name))
			if imgext[ext] {
				imageview(cwd, w, r)
				return
			} else if mdext[ext] {
				mdview(cwd, w, r)
				return
			} else {
				fileview(cwd, w, r)
				return
			}
		}
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
