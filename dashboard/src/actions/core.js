import chain from '../chain'
import { context } from '../utility/environment'
import actionCreator from './actionCreator'

const updateInfo = actionCreator('UPDATE_CORE_INFO', param => ({ param }))

const fetchCoreInfo = () => {
  return (dispatch) => {
    return chain.Core.info(context)
      .then((info) => dispatch(updateInfo(info)))
  }
}

const retry = (dispatch, promise, count = 10) => {
  return dispatch(promise).catch((err) => {
    var currentTime = new Date().getTime()
    while (currentTime + 200 >= new Date().getTime()) { /* wait for retry */ }

    if (count >= 1) {
      retry(dispatch, promise, count -1)
    } else {
      throw(err)
    }
  })
}

let actions = {
  updateInfo,
  fetchCoreInfo,
  submitConfiguration: (data) => {
    return (dispatch) => {
      // Convert string value to boolean for API
      data.is_generator = data.is_generator === 'true' ? true : false
      data.is_signer = data.is_generator

      return chain.Core.configure(data, context)
        .then(() => retry(dispatch, fetchCoreInfo()))
    }
  }
}

export default actions
