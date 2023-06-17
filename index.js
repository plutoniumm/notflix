import { readdirSync, createReadStream, statSync, appendFile } from "fs";
import { createServer } from 'http';
import path, { resolve } from "path";

const server = createServer( ( req, res ) => {
  if ( req.method === 'GET' && req.url === "/" ) {
    res.writeHead( 200, {
      'Content-Type': 'text/html',
      'Content-Security-Policy': "default-src *  data: 'unsafe-inline' 'unsafe-eval'; script-src * data: 'unsafe-inline' 'unsafe-eval'; img-src * data: 'unsafe-inline'; font-src * data: 'unsafe-inline' 'unsafe-eval'; worker-src * data: blob: 'unsafe-inline' 'unsafe-eval';"
    } );
    return createReadStream( resolve( "index.html" ) ).pipe( res );
  }
  if ( req.method === 'POST' && req.url.startsWith( "/assets" ) ) {
    const file = req.url.replace( "/assets", "assets" );
    console.log( file );
    return createReadStream( resolve( file ) ).pipe( res )
  };

  if ( req.method === 'POST' && req.url.startsWith( "/track" ) ) {
    const body = req.body;
    const name = req.url.replace( "/track/", "" );
    const data = `${ name } ${ body }\n`;
    // write to file
    appendFile( 'tests.txt', data, function ( err ) {
      if ( err ) throw err;
      console.log( 'Saved!' );
    } );
  }


  if ( req.method === 'GET' && req.url === "/list" ) {
    const files =
      readdirSync( resolve( "video" ) )
        .filter( file => !file.startsWith( "." ) );
    res.writeHead( 200, {
      'Content-Type': 'application/json',
    } );
    return res.end( JSON.stringify( files ) );
  }

  if ( req.method === 'GET' && req.url === "/video" ) {
    const filepath = resolve( "video/video.mp4" );
    const fileSize = statSync( filepath )?.size
    const range = req.headers?.range

    if ( range ) {
      // range format is "bytes=start-end",
      const parts = range.replace( /bytes=/, "" ).split( "-" );

      const start = parseInt( parts[ 0 ], 10 );
      const end = parts[ 1 ] ? parseInt( parts[ 1 ], 10 ) : fileSize - 1; //IF NO END => EOF

      const chunksize = ( end - start ) + 1
      /*we can provide offset values as options to
     the fs.createReadStream to read part of content*/
      let file;
      try {
        file = createReadStream( filepath, { start, end } );
      } catch ( error ) {
        console.log( error );
        res.writeHead( 400 );
        return res.end( "bad request" );
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
    res.writeHead( 400 );
    res.end( "bad request" );
  }
} )

const PORT = process.env.PORT || 3000;
server.listen( PORT, () => console.log( `server listening on port:${ PORT }` ) )