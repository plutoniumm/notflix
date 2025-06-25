all:
	go run *.go;

run:
	./node_modules/.bin/rolldown src/index.ts --file ./public/assets/notflix.js --watch
