"use strict";

var dialog = document.getElementById( 'dialog' );
if ( dialog ) fetch( '/list' ).then( function ( res ) {
  return res.json();
} ).then( function ( data ) {
  console.log( data );
} );

function runCodeFunction ( num ) {
  // seek 10s back
  if ( num === 4 )
    return player.currentTime( player.currentTime() - 10 );
  // mock f5 for TV
  if ( num === 5 )
    return window.reload();
  // seek 10s forward
  if ( num === 6 )
    return player.currentTime( player.currentTime() + 10 );
}

function handleKey ( name, event ) {
  var key = event.key,
    code = event.code;
  fetch( "/track/" + name, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json'
    },
    body: JSON.stringify( {
      key: event.key,
      code: event.code,
      ctrlKey: event.ctrlKey,
      shiftKey: event.shiftKey,
      altKey: event.altKey
    } )
  } ).catch( function ( e ) {
    return console.log( e );
  } );

  if ( typeof player === "undefined" ) return 0;
  if ( key === "MediaPlayPause" ) player.paused() ? player.play() : player.pause();
  if ( code.startsWith( 'Digit' ) ) {
    var num = parseInt( code.replace( 'Digit', '' ), 10 );
    runCodeFunction( num );
  }
};
function handleClick ( name, event ) {
  var target = event.target;
  fetch( "/track/".concat( name ), {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json'
    },
    body: JSON.stringify( {
      x: event.clientX,
      y: event.clientY,
      target: target.tagName + ":" + target.id
    } )
  } )[ "catch" ]( function ( e ) {
    return console.log( e );
  } );
};


window.addEventListener( 'keyup', function ( e ) {
  return handleKey( "keyup", e );
} );
window.addEventListener( 'click', function ( e ) {
  return handleClick( "click", e );
} );