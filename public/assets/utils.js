const $ = ( s ) => document.querySelector( s );
const Sort = ( a, b ) => a.localeCompare( b, 'en', { numeric: true } );
const search = new URLSearchParams( window.location.search );

const get = ( url ) => fetch( url )
  .then( r => r.json() )
  .catch( e => console.log( `[GET error]: ${ url }`, e ) );

const del = ( url ) => fetch( url, { method: 'DELETE' } )
  .then( r => r.json() )
  .catch( e => console.log( `[DEL error]: ${ url }`, e ) );

// return if get is 200
const exists = ( url ) => fetch( url )
  .then( res => res.status === 200 );