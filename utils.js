import {
  writeFileSync, readFileSync,
  appendFileSync, appendFile
} from "fs";

export const CSP = [
  "default-src *  data: 'unsafe-inline' 'unsafe-eval';",
  "script-src * data: 'unsafe-inline' 'unsafe-eval';",
  "img-src * data: 'unsafe-inline';",
  "font-src * data: 'unsafe-inline' 'unsafe-eval';",
  "worker-src * data: blob: 'unsafe-inline' 'unsafe-eval'"
].join( " " );

export const fwrite = ( path, data ) => writeFileSync( path,
  typeof data === "string" ? data : JSON.stringify( data )
);
export const append = ( path, data ) => appendFileSync( path, data );
export const log = ( d ) => {
  appendFile( "tests.txt", `${ d }\n`, ( e ) => {
    if ( e ) console.log( "Log Err: " + e );
  } );
};