// external imports
import * as React from 'react'
import * as Brace from 'brace'
import { connect } from 'react-redux'
import AceEditor from 'react-ace'
import 'brace/theme/monokai'

// internal imports
import { setSource } from '../actions'

const mapStateToProps = undefined
const mapDispatchToProps = (dispatch) => {
  return {
    handleChange: (value) => {
      dispatch(setSource(value))
    }
  }
}

const Ace = ({ source, handleChange }) => {
  return (
    <div className="panel-body">
      <AceEditor
        mode="ivy"
        theme="monokai"
        onChange={handleChange}
        name="aceEditor"
        width="100%"
        tabSize={2}
        value={source}
        editorProps={{$blockScrolling: Infinity}}
        setOptions={{
          useSoftTabs: true,
          showPrintMargin: false,
          fontFamily: "Nitti, Menlo, Monaco, Consolas, Courier New, monospace",
          fontSize: 18
        }}
      />
    </div>
  )
}

export default connect(
  mapStateToProps,
  mapDispatchToProps
)(Ace)
