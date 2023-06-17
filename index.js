import { readdirSync, createReadStream, statSync, appendFile } from "fs";
import { createServer } from 'http';
import path, { resolve } from "path";

const CSP = [
  "default-src *  data: 'unsafe-inline' 'unsafe-eval';",
  "script-src * data: 'unsafe-inline' 'unsafe-eval';",
  "img-src * data: 'unsafe-inline';",
  "font-src * data: 'unsafe-inline' 'unsafe-eval';",
  "worker-src * data: blob: 'unsafe-inline' 'unsafe-eval'"
].join( " " );

const server = createServer( ( req, res ) => {
  console.log( 14, req.url );
  if ( req.method === 'GET' && req.url === "/" ) {
    res.writeHead( 200, {
      'Content-Type': 'text/html',
      'Content-Security-Policy': CSP
    } );
    return createReadStream( resolve( "index.html" ) ).pipe( res );
  }
  if ( req.method === 'GET' && req.url.startsWith( "/assets" ) ) {
    const file = req.url.replace( "/assets", "assets" );
    console.log( 24, file );
    return createReadStream( resolve( file ) ).pipe( res )
  };

  if ( req.method === 'POST' && req.url.startsWith( "/track" ) ) {
    let body = '';
    req.on( 'data', ( chunk ) => body += chunk );
    req.on( 'end', () => {
      console.log( 32, body );
      appendFile( 'tests.txt', `${ req.url.replace( '/track/', '' ) } ${ body }\n`, console.log );

      res.writeHead( 200, { 'Content-Type': 'text/html', } );
      return res.end( "ok" );
    } );
  }

  if ( req.method === 'GET' && req.url === "/list" ) {
    const files = readdirSync( resolve( "video" ) )
      .filter( file => !file.startsWith( "." ) );
    res.writeHead( 200, { 'Content-Type': 'application/json', } );
    return res.end( JSON.stringify( files ) );
  }

  if ( req.method === 'GET' && req.url === "/video" ) {
    const filepath = resolve( "video/video.mp4" );
    const fileSize = statSync( filepath )?.size;
    const range = req.headers?.range;

    if ( range ) {
      // format is "bytes=start-end",
      const parts = range.replace( /bytes=/, "" ).split( "-" );

      const start = parseInt( parts[ 0 ], 10 );
      const end = parts[ 1 ] ? parseInt( parts[ 1 ], 10 ) : fileSize - 1; //IF NO END => EOF

      const chunksize = ( end - start ) + 1;
      let file;
      try {
        file = createReadStream( filepath, { start, end } );
      } catch ( error ) {
        console.log( error );
        console.log( "Bad Request @ /video" );
        res.writeHead( 400 );
        return res.end( "Bad Request @ /video" );
      }

      /* 206 => partial content*/
      res.writeHead( 206, {
        'Content-Range': `bytes ${ start }-${ end }/${ fileSize }`,
        'Accept-Ranges': 'bytes',
        'Content-Length': chunksize,
        'Content-Type': 'video/mp4',
      } );
      file.pipe( res );
    } else {
      // IF NO RANGE
      res.writeHead( 200, {
        'Content-Length': fileSize,
        'Content-Type': 'video/mp4',
      } );
      createReadStream( path ).pipe( res );
    }
  } else {
    // For when you dont know wtf happened
    console.log( "Unkwnown Bad Request @ " + req.url );
    res.writeHead( 400 );
    res.end( "Unkwnown Bad Request @ " + req.url );
  }
} )

const PORT = process.env.PORT || 3000;
server.listen( PORT, () => console.log( `server listening on port:${ PORT }` ) )