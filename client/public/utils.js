function handleKey ( name, event ) {
  let { key, code } = event;

  fetch( "/track/" + name, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify( {
      key, code,
      ctrlKey: event.ctrlKey,
      shiftKey: event.shiftKey,
      altKey: event.altKey
    } )
  } ).catch( console.log );

  if ( typeof player === "undefined" ) return 0;
  if ( key === "MediaPlayPause" )
    player.paused() ? player.play() : player.pause();
};

function handleClick ( name, event ) {
  let { tagName, id } = event.target;

  fetch( "/track/".concat( name ), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify( {
      x: event.clientX,
      y: event.clientY,
      target: tagName + ":" + id
    } )
  } ).catch( console.log );
};

const video = document.querySelector( 'video' );
video.addEventListener( 'keyup', ( e ) => handleKey( "keyup", e ) );
video.addEventListener( 'click', ( e ) => handleClick( "click", e ) );