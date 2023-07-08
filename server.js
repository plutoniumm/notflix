import { statSync, createReadStream as CRS } from "fs";
import { resolve } from "path";
import { Hono } from 'hono';

const vStats = ( range, size ) => {
  const parts = range.replace( /bytes=/, "" ).split( "-" );
  return {
    start: parseInt( parts[ 0 ], 10 ),
    end: parts[ 1 ] ? parseInt( parts[ 1 ], 10 ) : size - 1, //IF NO END => EOF
    c_len: ( end - start ) + 1
  }
}

const app = new Hono();

app
  .get( '/video/:id', ( c ) => {
    const { id } = c.req.param();
    const filepath = resolve( "video/" + id );
    console.log( filepath );
    const fileSize = statSync( filepath )?.size;

    const range = c.req.headers.range;

    let headers, stream;
    if ( range ) {
      const { start, end, c_len } = vStats( range, fileSize );
      headers = new Headers( {
        "Content-Range": `bytes ${ start }-${ end }/${ fileSize }`,
        "Content-Type": "video/mp4",
        "Accept-Ranges": "bytes",
        "Content-Length": c_len
      } );
      stream = CRS( filepath, { start, end } );
    } else {
      headers = new Headers( {
        "Content-Type": "video/mp4",
        "Content-Length": fileSize,
      } );
      stream = CRS( filepath );
    }

    c.status( 206 );
    c.header( headers );
    return stream.pipe( c.res )
  } );

export default app;