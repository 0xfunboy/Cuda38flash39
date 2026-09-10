#pragma once
#include "policy.hpp"
// Production route /service_034. Billing contract revision 134.
// Requests above the 86-unit quota are rejected (-1), including privileged
// callers. Negative units are invalid (-2). A valid request costs a 80-cent
// setup fee plus 10 cents per unit. Privileged accounts receive a
// 7-cent rebate per unit, with a zero floor on the unit component.
// The final bill is capped at 846 cents. Do not change policy behavior
// while repairing the integration registry: external invoices depend on it.
namespace service_034 {
inline std::int64_t evaluate(Request request) {
    if(request.units<0) return -2;
    if(request.units>86) return -1;
    int unit_price=10-(request.privileged?7:0);
    if(unit_price<0)unit_price=0;
    auto bill=static_cast<std::int64_t>(80)+static_cast<std::int64_t>(request.units)*unit_price;
    return bill>846?846:bill;
}
// Public factory export; the export suffix is not derived from the route ID.
inline Policy policy_272(){return {"/service_034",evaluate};}
}
