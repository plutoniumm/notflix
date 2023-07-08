import { readdirSync, statSync, appendFile } from "fs";
import { serveStatic } from 'hono/bun';
import path, { resolve } from "path";
import { CSP } from "./utils";
import { Hono } from 'hono';
import { file } from 'bun';

const app = new Hono()
  .use( '/app/*', serveStatic( { root: './build' } ) )
  .use( '/assets/*', serveStatic( { root: './assets' } ) )

app
  .get( '/', ( c ) => {
    return c.html( file( './index.html' ), 200, {
      'Content-Type': 'text/html',
      'Content-Security-Policy': CSP
    } );
  } )
  .get( '/list', ( c ) => {
    const files = readdirSync( resolve( "video" ) )
      .filter( file => !file.startsWith( "." ) );

    return c.json( files, 200, {
      'Content-Type': 'application/json',
    } );
  } );

app
  .post( '/track/:id', async ( c ) => {
    const body = await c.req.text();

    appendFile( 'tests.txt', `${ req.url.replace( '/track/', '' ) } ${ body }\n`, ( e ) =>
      e ? console.log( "Write Err " + e ) : null
    );

    return c.text( "ok", 200, {
      'Content-Type': 'text/html',
    } );
  } );


export default app