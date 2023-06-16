import { createReadStream, statSync } from "fs";
import { createServer } from 'http';
import path, { resolve } from "path";

const server = createServer( ( req, res ) => {
  if ( req.method !== 'GET' && req.method !== 'HEAD' ) {
    res.writeHead( 405 );
    res.end( "method not allowed" );
    return 0;
  }
  if ( req.url === "/" ) {
    createReadStream( resolve( "index.html" ) ).pipe( res );
    return 0;
  }
  //if video content is requesting
  if ( req.url === "/video" ) {
    const filepath = resolve( "video/video.mp4" );
    const stat = statSync( filepath )
    const fileSize = stat.size
    const range = req.headers.range

    if ( range ) {
      //range format is "bytes=start-end",
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
        res.end( "bad request" );
        return 0;
      }

      const head = {
        'Content-Range': `bytes ${ start }-${ end }/${ fileSize }`,
        'Accept-Ranges': 'bytes',
        'Content-Length': chunksize,
        'Content-Type': 'video/mp4',
      }

      /* 206 => partial content*/
      res.writeHead( 206, head );
      file.pipe( res );
    } else {
      // IF NO RANGE
      const head = {
        'Content-Length': fileSize,
        'Content-Type': 'video/mp4',
      }
      res.writeHead( 200, head );
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