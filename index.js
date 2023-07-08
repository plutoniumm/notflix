import { readdirSync, createReadStream, statSync, appendFile } from "fs";
import { createServer } from 'http';
import path, { resolve } from "path";
const server = createServer( ( req, res ) => {
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
        console.log( "Bad Request @ /video", error );
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
    console.log( "Unkwnown Bad Request @ " + req.url, req.method );
    res.writeHead( 400 );
    res.end( "Unkwnown Bad Request @ " + req.url );
  }
} )

const PORT = process.env.PORT || 3000;
server.listen( PORT, () => console.log( `server listening on port:${ PORT }` ) )