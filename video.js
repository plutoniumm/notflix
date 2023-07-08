export const vStats = ( range, size ) => {
  const parts = range.replace( /bytes=/, "" ).split( "-" );
  return {
    start: parseInt( parts[ 0 ], 10 ),
    end: parts[ 1 ] ? parseInt( parts[ 1 ], 10 ) : size - 1, //IF NO END => EOF
    c_len: ( end - start ) + 1
  }
}